package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/boltdb/bolt"
	"github.com/bwmarrin/discordgo"
)

// Bot holds the bot state.
type Bot struct {
	db       *bolt.DB
	channels map[string]Channel
	games    map[GameIdentifier]Game
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

type Game struct {
	Players     map[Player]bool
	Red         map[string]bool
	Blue        map[string]bool
	RedCaptain  *Player
	BlueCaptain *Player
	mutex       *sync.Mutex
}

type Player struct {
	Name         string
	NotifyOnFill bool 
}

type selector func([]string) (string, int)

func (g Game) PlayerWithName(p string) (*Player) {
	for player := range g.Players {
		if player.Name == p {
			return &player
		}
	}

	return nil
}

func (b *Bot) Enable(s *discordgo.Session, m *discordgo.MessageCreate) {
	if _, ok := b.channels[m.ChannelID]; ok {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Pugbot was already enabled"))
	} else {
		c := Channel{make(map[string]Mod)}
		b.db.Update(func(tx *bolt.Tx) error {
			bucket, err := tx.CreateBucketIfNotExists([]byte("ChannelsBucket"))
			cSerialized, err := json.Marshal(c)
			err = bucket.Put([]byte(m.ChannelID), []byte(cSerialized))
			return err
		})
		b.channels[m.ChannelID] = c
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Pugbot enabled"))
	}
}

func (b *Bot) Addmod(s *discordgo.Session, m *discordgo.MessageCreate, name string, maxPlayers int) {
	if c, ok := b.channels[m.ChannelID]; ok {
		if _, ok := c.Mods[name]; ok {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Mod with this name already exists"))
		} else {
			mod := Mod{maxPlayers}
			c.Mods[name] = mod
			b.db.Update(func(tx *bolt.Tx) error {
				bucket, err := tx.CreateBucketIfNotExists([]byte("ChannelsBucket"))
				cSerialized, err := json.Marshal(c)
				err = bucket.Put([]byte(m.ChannelID), []byte(cSerialized))
				return err
			})
		}
	} else {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Pugbot was not enabled on this channel"))
	}
}

func (b *Bot) Join(s *discordgo.Session, m *discordgo.MessageCreate, name string) {
	if c, ok := b.channels[m.ChannelID]; ok {
		if mod, ok := c.Mods[name]; ok {
			g := GameIdentifier{m.ChannelID, name}
			if _, ok := b.games[g]; !ok {
				b.games[g] = Game{Players: make(map[Player]bool), Blue: make(map[string]bool), Red: make(map[string]bool), mutex: new(sync.Mutex)}
			}
			b.games[g].mutex.Lock()
			defer b.games[g].mutex.Unlock()
			
			if len(b.games[g].Players) == mod.MaxPlayers {
				return
			}
			if b.games[g].PlayerWithName(m.Author.Username) != nil {
				return
			}
			player := Player { m.Author.Username, false }
			b.games[g].Players[player] = true
			// TODO: Add logic to start a game.
		}
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
	if c, ok := b.channels[m.ChannelID]; ok {
		if _, ok := c.Mods[name]; ok {
			g := GameIdentifier{m.ChannelID, name}
			if _, ok := b.games[g]; !ok {
				return
			}

			for player := range b.games[g].Players {
				if player.Name == m.Author.Username {
					delete(b.games[g].Players, player)
					player.NotifyOnFill = true
					b.games[g].Players[player] = true
					s.MessageReactionAdd(m.ChannelID, m.Message.ID, "✅")
					break
				}
			}
		}
	}
}

func (b *Bot) Leave(s *discordgo.Session, m *discordgo.MessageCreate, name string) {
	if c, ok := b.channels[m.ChannelID]; ok {
		if _, ok := c.Mods[name]; ok {
			g := GameIdentifier{m.ChannelID, name}
			if _, ok := b.games[g]; !ok {
				return
			}
			b.games[g].mutex.Lock()
			defer b.games[g].mutex.Unlock()
			player := * b.games[g].PlayerWithName(m.Author.Username)
			delete(b.games[g].Players, player)
		}
	}
}

func (b *Bot) L(s *discordgo.Session, m *discordgo.MessageCreate, name string) {
	b.Leave(s, m, name)
}

func (b *Bot) List(s *discordgo.Session, m *discordgo.MessageCreate, name string) {
	if c, ok := b.channels[m.ChannelID]; ok {
		if mod, ok := c.Mods[name]; ok {
			g := GameIdentifier{m.ChannelID, name}
			if _, ok := b.games[g]; !ok {
				return
			}
			bot.games[g].mutex.Lock()
			defer bot.games[g].mutex.Unlock()
			var msg strings.Builder
			msg.Grow(32)
			fmt.Fprintf(&msg, "[%d / %d]\n", len(b.games[g].Players), mod.MaxPlayers)
			for player := range b.games[g].Players {
				fmt.Fprintf(&msg, "%s | ", player.Name)
			}
			s.ChannelMessageSend(m.ChannelID, msg.String())
		}
	}
}

func (b *Bot) Ls(s *discordgo.Session, m *discordgo.MessageCreate, name string) {
	b.List(s, m, name)
}
