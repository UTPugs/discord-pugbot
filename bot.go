package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/bwmarrin/discordgo"
	"github.com/jasonlvhit/gocron"
)

const DefaultTimeout = 5

// Bot holds the bot state.
type Bot struct {
	channels  map[string]*Channel
	games     map[GameIdentifier]*Game
	client    *firestore.Client
	ctx       context.Context
	scheduler *gocron.Scheduler
}

type Channel struct {
	Mods    map[string]*Mod
	Timeout int
}

type Mod struct {
	MaxPlayers int
}

type GameIdentifier struct {
	Channel string
	Mod     string
}

type PlayerMetadata struct {
	NotifyOnFill bool
	JoinTime     time.Time
	PickOrder    int
}

// Used for sorting for display
type Player struct {
	Key   string
	Value PlayerMetadata
}

type TeamColor bool

const (
	Red  TeamColor = false
	Blue           = true
)

// Bot commands

func (b *Bot) Enable(s *discordgo.Session, m *discordgo.MessageCreate) {
	if !isAdmin(s, m) {
		log.Printf("%s tried enabling bot on channel but is not an admin", m.Author.Username)
		return
	}
	if _, ok := b.channels[m.ChannelID]; ok {
		s.ChannelMessageSend(m.ChannelID, "Pugbot was already enabled")
	} else {
		c := Channel{make(map[string]*Mod), DefaultTimeout}
		b.client.Collection("channels").Doc(m.ChannelID).Set(b.ctx, c)
		b.channels[m.ChannelID] = &c
		s.ChannelMessageSend(m.ChannelID, "Pugbot enabled")
	}
}

func (b *Bot) Disable(s *discordgo.Session, m *discordgo.MessageCreate) {
	if !isAdmin(s, m) {
		log.Printf("%s tried enabling bot on channel but is not an admin", m.Author.Username)
		return
	}
	if _, ok := b.channels[m.ChannelID]; ok {
		delete(b.channels, m.ChannelID)
		s.ChannelMessageSend(m.ChannelID, "Pugbot disabled")
		b.client.Collection("channels").Doc(m.ChannelID).Delete(b.ctx)
		var gamesToDelete []GameIdentifier
		for game := range b.games {
			if game.Channel == m.ChannelID {
				gamesToDelete = append(gamesToDelete, game)
			}
		}
		for _, game := range gamesToDelete {
			delete(b.games, game)
		}
	}
}

func (b *Bot) Addmod(s *discordgo.Session, m *discordgo.MessageCreate, name string, maxPlayers int) {
	if !isAdmin(s, m) {
		log.Printf("%s tried adding mod on channel but is not an admin", m.Author.Username)
		return
	}
	if c, ok := b.channels[m.ChannelID]; ok {
		if _, ok := c.Mods[name]; ok {
			s.ChannelMessageSend(m.ChannelID, "Mod with this name already exists")
			log.Println("Mod with this name already exists")
		} else if maxPlayers%2 != 0 || maxPlayers == 0 {
			s.ChannelMessageSend(m.ChannelID, "Invalid player count")
			log.Println("Invalid player count")
		} else {
			mod := Mod{maxPlayers}
			c.Mods[name] = &mod
			g := GameIdentifier{m.ChannelID, name}
			b.games[g] = &Game{Players: make(map[string]*PlayerMetadata), Red: make(map[string]bool), Blue: make(map[string]bool), mutex: new(sync.Mutex), RedCaptain: new(string), BlueCaptain: new(string)}
			_, err := b.client.Collection("channels").Doc(m.ChannelID).Set(b.ctx, map[string]interface{}{
				"Mods": c.Mods,
			}, firestore.MergeAll)
			if err != nil {
				// Handle any errors in an appropriate way, such as returning them.
				log.Printf("An error has occurred: %s", err)
				return
			}
			s.MessageReactionAdd(m.ChannelID, m.Message.ID, "✅")
		}
	} else {
		s.ChannelMessageSend(m.ChannelID, "Pugbot is not enabled on this channel")
	}
}

func (b *Bot) Settimeout(s *discordgo.Session, m *discordgo.MessageCreate, timeoutInHours int) {
	if !isAdmin(s, m) {
		log.Printf("%s tried setting timeout on channel but is not an admin", m.Author.Username)
		return
	}
	if c, ok := b.channels[m.ChannelID]; ok {
		c.Timeout = timeoutInHours
		_, err := b.client.Collection("channels").Doc(m.ChannelID).Set(b.ctx, map[string]interface{}{
			"Timeout": timeoutInHours,
		}, firestore.MergeAll)
		if err != nil {
			// Handle any errors in an appropriate way, such as returning them.
			log.Printf("An error has occurred: %s", err)
			return
		}
		s.ChannelMessageSend(m.ChannelID, "Timeout set")
	}
}

func (b *Bot) Gettimeout(s *discordgo.Session, m *discordgo.MessageCreate) {
	if c, ok := b.channels[m.ChannelID]; ok {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Timeout is set to %d minutes", c.Timeout))
	}
}

func (b *Bot) Join(s *discordgo.Session, m *discordgo.MessageCreate, name string) {
	b.Addplayer(s, m, name, m.Author.Username)
}

func (b *Bot) Addplayer(s *discordgo.Session, m *discordgo.MessageCreate, name string, playerNames ...string) {
	gameID, mod := b.GameInfo(m.ChannelID, name)
	if gameID == nil || mod == nil {
		return
	}
	if m.Author.Username != playerNames[0] {
		if !isAdmin(s, m) {
			log.Printf("%s tried adding player %s but is not an admin", m.Author.Username, playerNames[0])
			return
		}
	}

	if _, ok := b.games[*gameID]; !ok {
		b.games[*gameID] = &Game{Players: make(map[string]*PlayerMetadata), Blue: make(map[string]bool), Red: make(map[string]bool), mutex: new(sync.Mutex), RedCaptain: new(string), BlueCaptain: new(string)}
	}
	game := b.games[*gameID]

	game.mutex.Lock()
	defer game.mutex.Unlock()
	for _, playerName := range playerNames {
		if game.IsFull(mod) {
			continue
		}
		game.AddPlayer(playerName)
	}

	if game.IsFull(mod) {
		game.BeginPicks(s, m.ChannelID, name, mod)
	} else {
		b.List(s, m, name)
	}
}

func (b *Bot) Reset(s *discordgo.Session, m *discordgo.MessageCreate, name string) {
	if !isAdmin(s, m) {
		log.Printf("%s tried resetting but is not an admin", m.Author.Username)
		return
	}
	gameID, mod := b.GameInfo(m.ChannelID, name)
	if gameID == nil || mod == nil {
		return
	}
	if game, ok := b.games[*gameID]; ok {
		s.ChannelMessageSend(m.ChannelID, "Reset!")
		game.AddPlayer(*game.RedCaptain)
		game.AddPlayer(*game.BlueCaptain)
		game.Red = make(map[string]bool)
		game.Blue = make(map[string]bool)
		game.RedCaptain = new(string)
		game.BlueCaptain = new(string)
		if game.IsFull(mod) {
			game.BeginPicks(s, m.ChannelID, name, mod)
		} else {
			b.List(s, m, name)
		}
	}
}

func (b *Bot) Teams(s *discordgo.Session, m *discordgo.MessageCreate, name string) {
	gameID, mod := b.GameInfo(m.ChannelID, name)
	if gameID == nil || mod == nil {
		return
	}
	if game, ok := b.games[*gameID]; ok {
		if !game.IsPickingTeams(mod) {
			return
		}
		b.teams(s, m, *gameID)
	}
}

func (b *Bot) Pn(s *discordgo.Session, m *discordgo.MessageCreate, playerNames ...string) {
	if c, ok := b.channels[m.ChannelID]; ok {
		count := 0
		var pickingModName string
		for modName := range c.Mods {
			gameID, mod := b.GameInfo(m.ChannelID, modName)
			if _, ok := b.games[*gameID]; !ok {
				continue
			}
			game := b.games[*gameID]

			if !game.IsPickingTeams(mod) {
				continue
			}
			pickingModName = modName
			count++
		}
		if count == 1 {
			b.Pickname(s, m, pickingModName, playerNames...)
		} else if count > 1 {
			s.ChannelMessageSend(m.ChannelID, "More than one game running in parallel, picking use .pick <mod> <player>")
		}
	}
}

func (b *Bot) Pickname(s *discordgo.Session, m *discordgo.MessageCreate, modName string, playerNames ...string) {
	gameID, mod := b.GameInfo(m.ChannelID, modName)
	if gameID == nil || mod == nil || len(playerNames) > 2 {
		return
	}

	if _, ok := b.games[*gameID]; !ok {
		return
	}

	game := b.games[*gameID]

	if !game.IsPickingTeams(mod) {
		return
	}

	for _, playerName := range playerNames {
		if !game.HasPlayer(playerName) {
			return
		}
	}

	// 0 is red, 1 is blue
	for _, playerName := range playerNames {
		pickColor := game.PickColor()
		if pickColor == Red && (*game.RedCaptain == m.Message.Author.Username || m.Message.Author.Username == "hyperreal") {
			game.Red[playerName] = true
			delete(game.Players, playerName)
		}
		if pickColor == Blue && (*game.BlueCaptain == m.Message.Author.Username || m.Message.Author.Username == "hyperreal") {
			game.Blue[playerName] = true
			delete(game.Players, playerName)
		}
	}
	if len(game.Players) == 1 {
		lastPlayer := getAnyPlayer(game.Players)
		delete(game.Players, lastPlayer)
		if len(game.Red) < len(game.Blue) {
			game.Red[lastPlayer] = true
		} else {
			game.Blue[lastPlayer] = true
		}
		b.teamsSelected(s, m, *gameID)
	} else {
		nextPickColor := game.PickColor()
		toPick := *game.BlueCaptain
		if nextPickColor == Red {
			toPick = *game.RedCaptain
		}
		b.List(s, m, modName)
		b.teams(s, m, *gameID)
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("%s to pick", toPick))
	}
}

func (b *Bot) J(s *discordgo.Session, m *discordgo.MessageCreate, name string) {
	b.Join(s, m, name)
}

func (b *Bot) Jp(s *discordgo.Session, m *discordgo.MessageCreate, name string) {
	b.Joinpm(s, m, name)
}

func (b *Bot) Joinpm(s *discordgo.Session, m *discordgo.MessageCreate, name string) {
	b.Join(s, m, name)
	b.Pm(s, m, name)
}

func (b *Bot) Pm(s *discordgo.Session, m *discordgo.MessageCreate, name string) {
	gameID, mod := b.GameInfo(m.ChannelID, name)
	if gameID == nil || mod == nil {
		return
	}

	if _, ok := b.games[*gameID]; !ok {
		return
	}

	game := b.games[*gameID]

	if metadata, ok := game.Players[m.Author.Username]; ok {
		if !metadata.NotifyOnFill {
			game.mutex.Lock()
			defer game.mutex.Unlock()
			metadata.NotifyOnFill = true
			game.Players[m.Author.Username] = metadata
			s.MessageReactionAdd(m.ChannelID, m.Message.ID, "✅")
		}
	}
}

func (b *Bot) Leave(s *discordgo.Session, m *discordgo.MessageCreate, name string) {
	gameID, mod := b.GameInfo(m.ChannelID, name)
	if gameID == nil || mod == nil {
		return
	}

	if _, ok := b.games[*gameID]; !ok {
		return
	}

	game := b.games[*gameID]
	if _, ok := game.Players[m.Author.Username]; ok {
		game.mutex.Lock()
		defer game.mutex.Unlock()
		delete(game.Players, m.Author.Username)
		b.List(s, m, name)
	}
}

func (b *Bot) L(s *discordgo.Session, m *discordgo.MessageCreate, name string) {
	b.Leave(s, m, name)
}

func (b *Bot) Leaveall(s *discordgo.Session, m *discordgo.MessageCreate) {
	if c, ok := b.channels[m.ChannelID]; ok {
		for name := range c.Mods {
			g := GameIdentifier{m.ChannelID, name}
			if _, ok := b.games[g]; !ok {
				return
			}

			b.games[g].mutex.Lock()
			defer b.games[g].mutex.Unlock()
			if _, ok := b.games[g].Players[m.Author.Username]; ok {
				delete(b.games[g].Players, m.Author.Username)
				b.List(s, m, name)
			}
		}
	}
}

func (b *Bot) Lva(s *discordgo.Session, m *discordgo.MessageCreate) {
	b.Leaveall(s, m)
}

func (b *Bot) List(s *discordgo.Session, m *discordgo.MessageCreate, name string) {
	gameID, mod := b.GameInfo(m.ChannelID, name)
	if gameID == nil || mod == nil {
		return
	}

	if _, ok := b.games[*gameID]; !ok {
		return
	}

	game := b.games[*gameID]

	var msg strings.Builder
	fmt.Fprintf(&msg, "**%s** [%d / %d]\n", name, len(game.Players), mod.MaxPlayers)
	fmt.Fprintf(&msg, game.BuildPlayerList())

	s.ChannelMessageSend(m.ChannelID, msg.String())
}

func (b *Bot) Lsa(s *discordgo.Session, m *discordgo.MessageCreate) {
	b.ListAll(s, m)
}

func (b *Bot) ListAll(s *discordgo.Session, m *discordgo.MessageCreate) {
	if c, ok := b.channels[m.ChannelID]; ok {
		var modLists []string
		for modName, mod := range c.Mods {
			g := GameIdentifier{m.ChannelID, modName}
			if _, ok := b.games[g]; !ok {
				continue
			}
			modList := fmt.Sprintf("**%s** [%d / %d]", modName, len(b.games[g].Players), mod.MaxPlayers)
			modLists = append(modLists, modList)
		}

		output := strings.Join(modLists, " | ")
		s.ChannelMessageSend(m.ChannelID, output)
	}
}

func (b *Bot) Ls(s *discordgo.Session, m *discordgo.MessageCreate, name string) {
	b.List(s, m, name)
}

func (b *Bot) Captain(s *discordgo.Session, m *discordgo.MessageCreate) {
	if c, ok := b.channels[m.ChannelID]; ok {
		for modName, mod := range c.Mods {
			g := GameIdentifier{m.ChannelID, modName}
			if game, ok := b.games[g]; ok {
				if game.HasPlayer(m.Author.Username) && game.IsFull(mod) && !game.IsPickingTeams(mod) {
					log.Printf(fmt.Sprintf("Setting captain to %s for %p", m.Author.Username, game))
					s.ChannelMessageSend(m.ChannelID, game.SetNextCaptainIfPossible(m.Author.Username))
					return
				}
			}
		}
	}
}

func (b *Bot) Forcerandomcaptains(s *discordgo.Session, m *discordgo.MessageCreate, name string) {
	if !isAdmin(s, m) {
		log.Printf("%s tried forcing random captains but is not an admin", m.Author.Username)
		return
	}
	g := GameIdentifier{m.ChannelID, name}
	if game, ok := b.games[g]; ok {
		game.AutoPickRemainingCaptains(s, m.ChannelID)
	}
}

func (b *Bot) Frc(s *discordgo.Session, m *discordgo.MessageCreate, name string) {
	b.Forcerandomcaptains(s, m, name)
}

// Internal

func (b *Bot) teamsSelected(s *discordgo.Session, m *discordgo.MessageCreate, g GameIdentifier) {
	s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Teams for %s were selected:", g.Mod))
	b.teams(s, m, g)
	b.games[g] = &Game{Players: make(map[string]*PlayerMetadata), Red: make(map[string]bool), Blue: make(map[string]bool), mutex: new(sync.Mutex), RedCaptain: new(string), BlueCaptain: new(string)}
}

func (b *Bot) teams(s *discordgo.Session, m *discordgo.MessageCreate, g GameIdentifier) {
	var msg strings.Builder
	msg.Grow(32)
	fmt.Fprintf(&msg, "Red: ")
	for player := range b.games[g].Red {
		fmt.Fprintf(&msg, "%s | ", player)
	}
	s.ChannelMessageSend(m.ChannelID, msg.String())
	msg.Reset()
	msg.Grow(32)
	fmt.Fprintf(&msg, "Blue: ")
	for player := range b.games[g].Blue {
		fmt.Fprintf(&msg, "%s | ", player)
	}
	s.ChannelMessageSend(m.ChannelID, msg.String())
}

func (b *Bot) GameInfo(channelID string, modName string) (*GameIdentifier, *Mod) {
	if channel, ok := b.channels[channelID]; ok {
		gameID := GameIdentifier{channelID, modName}
		if _, ok := b.games[gameID]; ok {
			mod := channel.Mods[modName]
			return &gameID, mod
		}
	}

	return nil, nil
}

func isAdmin(s *discordgo.Session, m *discordgo.MessageCreate) bool {
	if m.Author.Username == "hyperreal" {
		return true
	}
	for _, roleID := range m.Member.Roles {
		role, _ := s.State.Role(m.GuildID, roleID)
		if role.Name == "Admin" {
			return true
		}
	}
	return false
}

func (b *Bot) cleanupPlayers(s *discordgo.Session) {
	for k, game := range b.games {
		channel := b.channels[k.Channel]
		mod := channel.Mods[k.Mod]
		if game.IsPickingTeams(mod) {
			continue
		}
		game.mutex.Lock()
		defer game.mutex.Unlock()
		var playersToDelete []string
		for name, player := range game.Players {
			// log.Printf("%s considering", name)
			if player.JoinTime.Before(time.Now().Add(time.Duration(-channel.Timeout) * time.Minute)) {
				log.Printf("%s timed out", name)
				s.ChannelMessageSend(k.Channel, fmt.Sprintf("%s was removed from %s because they timed out", name, k.Mod))
				playersToDelete = append(playersToDelete, name)
			}
		}
		for _, player := range playersToDelete {
			delete(game.Players, player)
		}
	}
}

func (b *Bot) keepAlive(name string) {
	for k, game := range b.games {
		channel := b.channels[k.Channel]
		mod := channel.Mods[k.Mod]
		if game.IsPickingTeams(mod) {
			continue
		}
		for k, player := range game.Players {
			if k == name {
				player.JoinTime = time.Now()
			}
		}
	}
}

func getAnyPlayer(m map[string]*PlayerMetadata) string {
	for k := range m {
		return k
	}
	return ""
}
