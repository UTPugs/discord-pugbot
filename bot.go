package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/boltdb/bolt"
	"github.com/bwmarrin/discordgo"
)

// Bot holds the bot state.
type Bot struct {
	db       *bolt.DB
	channels map[string]Channel
	games    map[GameIdentifier]Game
}

// UserQuotes stores quotes of a particular user.
type UserQuotes struct {
	Quotes   []string
	Username string
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
	Players     map[string]bool
	Red         map[string]bool
	Blue        map[string]bool
	RedCaptain  string
	BlueCaptain string
}

type selector func([]string) (string, int)

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
			if _, ok := bot.games[g]; !ok {
				bot.games[g] = Game{make(map[string]bool), make(map[string]bool), make(map[string]bool), "", ""}
			}
			if len(bot.games[g].Players) == mod.MaxPlayers {
				return
			}
			bot.games[g].Players[m.Author.Username] = true
			// TODO: Add logic to start a game.
		}
	}
}

func (b *Bot) Leave(s *discordgo.Session, m *discordgo.MessageCreate, name string) {
	if c, ok := b.channels[m.ChannelID]; ok {
		if _, ok := c.Mods[name]; ok {
			g := GameIdentifier{m.ChannelID, name}
			if _, ok := bot.games[g]; !ok {
				return
			}
			delete(bot.games[g].Players, m.Author.Username)
		}
	}
}

func (b *Bot) List(s *discordgo.Session, m *discordgo.MessageCreate, name string) {
	if c, ok := b.channels[m.ChannelID]; ok {
		if mod, ok := c.Mods[name]; ok {
			g := GameIdentifier{m.ChannelID, name}
			if _, ok := bot.games[g]; !ok {
				return
			}
			var msg strings.Builder
			msg.Grow(32)
			fmt.Fprintf(&msg, "[%d / %d]\n", len(bot.games[g].Players), mod.MaxPlayers)
			for player := range bot.games[g].Players {
				fmt.Fprintf(&msg, "%s | ", player)
			}
			s.ChannelMessageSend(m.ChannelID, msg.String())
		}
	}
}

func (b *Bot) Ls(s *discordgo.Session, m *discordgo.MessageCreate, name string) {
	bot.List(s, m, name)
}
