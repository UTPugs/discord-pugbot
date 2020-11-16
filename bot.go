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
)

// Bot holds the bot state.
type Bot struct {
	channels map[string]Channel
	games    map[GameIdentifier]Game
	client   *firestore.Client
	ctx      context.Context
}

type Channel struct {
	Mods map[string]Mod
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
}

// Used for sorting for display
type Player struct {
	Key   string
	Value PlayerMetadata
}

// Bot commands

func (b *Bot) Enable(s *discordgo.Session, m *discordgo.MessageCreate) {
	if !isAdmin(s, m) {
		log.Printf("%s tried enabling bot on channel but is not an admin", m.Author.Username)
		return
	}
	if _, ok := b.channels[m.ChannelID]; ok {
		s.ChannelMessageSend(m.ChannelID, "Pugbot was already enabled")
	} else {
		c := Channel{make(map[string]Mod)}
		b.client.Collection("channels").Doc(m.ChannelID).Set(b.ctx, c)
		b.channels[m.ChannelID] = c
		s.ChannelMessageSend(m.ChannelID, "Pugbot enabled")
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
			c.Mods[name] = mod
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

func (b *Bot) Join(s *discordgo.Session, m *discordgo.MessageCreate, name string) {
	b.Addplayer(s, m, name, m.Author.Username)
}

func (b *Bot) Addplayer(s *discordgo.Session, m *discordgo.MessageCreate, name string, playerName string) {
	gameID, mod := b.GameInfo(m.ChannelID, name)
	if gameID == nil || mod == nil {
		return
	}
	if m.Author.Username != playerName {
		if !isAdmin(s, m) {
			log.Printf("%s tried adding player %s but is not an admin", m.Author.Username, playerName)
			return
		}
	}

	if _, ok := b.games[*gameID]; !ok {
		b.games[*gameID] = Game{Players: make(map[string]PlayerMetadata), Blue: make(map[string]bool), Red: make(map[string]bool), mutex: new(sync.Mutex), RedCaptain: new(string), BlueCaptain: new(string)}
	}
	game := b.games[*gameID]

	if game.IsFull(mod) {
		return
	}

	game.mutex.Lock()
	game.AddPlayer(playerName)

	if game.IsFull(mod) {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("%s has filled", name))
		if mod.MaxPlayers == 2 {
			for player := range game.Players {
				if *game.RedCaptain == "" {
					*game.RedCaptain = player
					game.Red[player] = true
				} else {
					*game.BlueCaptain = player
					game.Blue[player] = true
				}
			}
			b.teamsSelected(s, m, *gameID)
		} else {
			players := game.Players
			*game.RedCaptain = game.RandPlayer()
			game.Red[*game.RedCaptain] = true
			delete(players, *game.RedCaptain)
			*game.BlueCaptain = game.RandPlayer()
			game.Blue[*game.BlueCaptain] = true
			delete(players, *game.BlueCaptain)
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("%s is captain for red team, %s is captain for blue team", *game.RedCaptain, *game.BlueCaptain))
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("%s to pick", *game.RedCaptain))
			s.ChannelMessageSend(m.ChannelID, game.BuildPlayerList())
		}
		game.mutex.Unlock()
	} else {
		game.mutex.Unlock()
		b.List(s, m, name)
	}
}

func (b *Bot) Pick(s *discordgo.Session, m *discordgo.MessageCreate, modName string, playerName string) {
	gameID, mod := b.GameInfo(m.ChannelID, modName)
	if gameID == nil || mod == nil {
		return
	}

	if _, ok := b.games[*gameID]; !ok {
		return
	}

	game := b.games[*gameID]

	if !game.IsPickingTeams(mod) {
		return
	}

	if !game.HasPlayer(playerName) {
		return
	}

	// 0 is red, 1 is blue
	pickColor := 0
	pickedCount := len(game.Red) + len(game.Blue)
	if pickedCount > 0 && ((pickedCount-1)/2)%2 == 1 {
		pickColor = 1
	}
	if pickColor == 0 && (*game.RedCaptain == m.Message.Author.Username || m.Message.Author.Username == "hyperreal") {
		game.Red[playerName] = true
		delete(game.Players, playerName)
	}
	if pickColor == 1 && (*game.BlueCaptain == m.Message.Author.Username || m.Message.Author.Username == "hyperreal") {
		game.Blue[playerName] = true
		delete(game.Players, playerName)
	}
	if len(game.Players) == 1 {
		if len(game.Red) < len(game.Blue) {
			game.Red[playerName] = true
		} else {
			game.Blue[playerName] = true
		}
		b.teamsSelected(s, m, *gameID)
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
	game.mutex.Lock()
	if _, ok := game.Players[m.Author.Username]; ok {
		delete(game.Players, m.Author.Username)
		game.mutex.Unlock()
		b.List(s, m, name)
	}
}

func (b *Bot) L(s *discordgo.Session, m *discordgo.MessageCreate, name string) {
	b.Leave(s, m, name)
}

func (b *Bot) Leaveall(s *discordgo.Session, m *discordgo.MessageCreate) {
	if c, ok := b.channels[m.ChannelID]; ok {
		for name, _ := range c.Mods {
			g := GameIdentifier{m.ChannelID, name}
			if _, ok := b.games[g]; !ok {
				return
			}

			b.games[g].mutex.Lock()
			if _, ok := b.games[g].Players[m.Author.Username]; ok {
				delete(b.games[g].Players, m.Author.Username)
				b.games[g].mutex.Unlock()
				b.List(s, m, name)
			}
		}
	}
}

func (b *Bot) Lva(s *discordgo.Session, m *discordgo.MessageCreate, name string) {
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
	game.mutex.Lock()
	defer game.mutex.Unlock()

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

// Internal

func (b *Bot) teamsSelected(s *discordgo.Session, m *discordgo.MessageCreate, g GameIdentifier) {
	s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Teams for %s were selected:", g.Mod))
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
			return &gameID, &mod
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
