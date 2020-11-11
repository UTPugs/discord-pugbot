package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

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
	Players     map[string]PlayerMetadata
	Red         map[string]bool
	Blue        map[string]bool
	RedCaptain  *string
	BlueCaptain *string
	mutex       *sync.Mutex
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

func (b *Bot) Enable(s *discordgo.Session, m *discordgo.MessageCreate) {
	if _, ok := b.channels[m.ChannelID]; ok {
		s.ChannelMessageSend(m.ChannelID, "Pugbot was already enabled")
	} else {
		c := Channel{make(map[string]Mod)}
		b.db.Update(func(tx *bolt.Tx) error {
			bucket, err := tx.CreateBucketIfNotExists([]byte("ChannelsBucket"))
			cSerialized, err := json.Marshal(c)
			err = bucket.Put([]byte(m.ChannelID), []byte(cSerialized))
			return err
		})
		b.channels[m.ChannelID] = c
		s.ChannelMessageSend(m.ChannelID, "Pugbot enabled")
	}
}

func (b *Bot) Addmod(s *discordgo.Session, m *discordgo.MessageCreate, name string, maxPlayers int) {
	if c, ok := b.channels[m.ChannelID]; ok {
		if _, ok := c.Mods[name]; ok {
			s.ChannelMessageSend(m.ChannelID, "Mod with this name already exists")
		} else {
			mod := Mod{maxPlayers}
			c.Mods[name] = mod
			b.db.Update(func(tx *bolt.Tx) error {
				bucket, err := tx.CreateBucketIfNotExists([]byte("ChannelsBucket"))
				cSerialized, err := json.Marshal(c)
				err = bucket.Put([]byte(m.ChannelID), []byte(cSerialized))
				return err
			})
			s.MessageReactionAdd(m.ChannelID, m.Message.ID, "✅")
		}
	} else {
		s.ChannelMessageSend(m.ChannelID, "Pugbot was not enabled on this channel")
	}
}

func (b *Bot) Join(s *discordgo.Session, m *discordgo.MessageCreate, name string) {
	b.Addplayer(s, m, name, m.Author.Username)
}

func (b *Bot) Addplayer(s *discordgo.Session, m *discordgo.MessageCreate, name string, playerName string) {
	if c, ok := b.channels[m.ChannelID]; ok {
		if mod, ok := c.Mods[name]; ok {
			g := GameIdentifier{m.ChannelID, name}
			if _, ok := b.games[g]; !ok {
				b.games[g] = Game{Players: make(map[string]PlayerMetadata), Blue: make(map[string]bool), Red: make(map[string]bool), mutex: new(sync.Mutex), RedCaptain: new(string), BlueCaptain: new(string)}
			}
			if len(b.games[g].Players)+len(b.games[g].Red)+len(b.games[g].Blue) == mod.MaxPlayers {
				return
			}

			b.games[g].mutex.Lock()

			if _, ok := b.games[g].Players[playerName]; !ok {
				b.games[g].Players[playerName] = PlayerMetadata{JoinTime: time.Now()}
			}
			if len(b.games[g].Players)+len(b.games[g].Red)+len(b.games[g].Blue) == mod.MaxPlayers {
				s.ChannelMessageSend(m.ChannelID, "Pugbot has filled")
				if mod.MaxPlayers == 2 {
					for player := range b.games[g].Players {
						if *b.games[g].RedCaptain == "" {
							*b.games[g].RedCaptain = player
							b.games[g].Red[player] = true
						} else {
							*b.games[g].BlueCaptain = player
							b.games[g].Blue[player] = true
						}
					}
					b.teamsSelected(s, m, g)
				} else {
					players := b.games[g].Players
					*b.games[g].RedCaptain = RandPlayer(players)
					b.games[g].Red[*b.games[g].RedCaptain] = true
					delete(players, *b.games[g].RedCaptain)
					*b.games[g].BlueCaptain = RandPlayer(players)
					b.games[g].Blue[*b.games[g].BlueCaptain] = true
					delete(players, *b.games[g].BlueCaptain)
					s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("%s is captain for red team, %s is captain for blue team", *b.games[g].RedCaptain, *b.games[g].BlueCaptain))
					s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("%s to pick", *b.games[g].RedCaptain))
					var msg strings.Builder
					msg.Grow(32)
					for player := range b.games[g].Players {
						fmt.Fprintf(&msg, "%s | ", player)
					}
					s.ChannelMessageSend(m.ChannelID, msg.String())
				}
				b.games[g].mutex.Unlock()
			} else {
				b.games[g].mutex.Unlock()
				b.List(s, m, name)
			}
		}
	}
}

func (b *Bot) Pick(s *discordgo.Session, m *discordgo.MessageCreate, modName string, playerName string) {
	if c, ok := b.channels[m.ChannelID]; ok {
		g := GameIdentifier{m.ChannelID, modName}
		if _, ok := b.games[g]; ok {
			mod := c.Mods[modName]
			// Game found, check whether it's filled.
			if len(b.games[g].Players)+len(b.games[g].Red)+len(b.games[g].Blue) != mod.MaxPlayers {
				return
			}
			if _, ok := b.games[g].Players[playerName]; !ok {
				return
			}
			// 0 is red, 1 is blue
			pickColor := 0
			pickedCount := len(b.games[g].Red) + len(b.games[g].Blue)
			if pickedCount > 0 && ((pickedCount-1)/2)%2 == 1 {
				pickColor = 1
			}
			if pickColor == 0 && (*b.games[g].RedCaptain == m.Message.Author.Username || m.Message.Author.Username == "hyperreal") {
				b.games[g].Red[playerName] = true
				delete(b.games[g].Players, playerName)
			}
			if pickColor == 1 && (*b.games[g].BlueCaptain == m.Message.Author.Username || m.Message.Author.Username == "hyperreal") {
				b.games[g].Blue[playerName] = true
				delete(b.games[g].Players, playerName)
			}
			if len(b.games[g].Players) == 1 {
				if len(b.games[g].Red) < len(b.games[g].Blue) {
					b.games[g].Red[playerName] = true
				} else {
					b.games[g].Blue[playerName] = true
				}
				b.teamsSelected(s, m, g)
			}
		}
	}
}

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

func RandPlayer(players map[string]PlayerMetadata) string {
	keys := reflect.ValueOf(players).MapKeys()
	return keys[rand.Intn(len(keys))].String()
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

			if metadata, ok := b.games[g].Players[m.Author.Username]; ok {
				if !metadata.NotifyOnFill {
					metadata.NotifyOnFill = true
					b.games[g].Players[m.Author.Username] = metadata
					s.MessageReactionAdd(m.ChannelID, m.Message.ID, "✅")
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
			if _, ok := b.games[g].Players[m.Author.Username]; ok {
				delete(b.games[g].Players, m.Author.Username)
				b.games[g].mutex.Unlock()
				b.List(s, m, name)
			}
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
			b.games[g].mutex.Lock()
			defer b.games[g].mutex.Unlock()

			var msg strings.Builder
			fmt.Fprintf(&msg, "**%s** [%d / %d]\n", name, len(b.games[g].Players), mod.MaxPlayers)

			var sortedPlayers []Player
			for key, value := range b.games[g].Players {
				sortedPlayers = append(sortedPlayers, Player{key, value})
			}

			sort.Slice(sortedPlayers, func(i, j int) bool {
				return sortedPlayers[i].Value.JoinTime.Before(sortedPlayers[j].Value.JoinTime)
			})

			var sortedPlayerNames []string
			for _, player := range sortedPlayers {
				sortedPlayerNames = append(sortedPlayerNames, player.Key)
			}

			playerList := strings.Join(sortedPlayerNames, " | ")
			fmt.Fprintf(&msg, playerList)

			s.ChannelMessageSend(m.ChannelID, msg.String())
		}
	}
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
				return
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
