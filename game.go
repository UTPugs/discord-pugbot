package main

import (
	"fmt"
	"log"
	"math/rand"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

type Game struct {
	Players     map[string]*PlayerMetadata
	Red         map[string]bool
	Blue        map[string]bool
	RedCaptain  *string
	BlueCaptain *string
	mutex       *sync.Mutex
}

func (game *Game) IsPickingTeams(mod *Mod) bool {
	return game.IsFull(mod) && *game.RedCaptain != "" && *game.BlueCaptain != ""
}

func (game *Game) IsFull(mod *Mod) bool {
	return len(game.Players)+len(game.Red)+len(game.Blue) == mod.MaxPlayers
}

func (game *Game) HasPlayer(playerName string) bool {
	if _, ok := game.Players[playerName]; !ok {
		return false
	}

	return true
}

func (game *Game) AddPlayer(playerName string) {
	if _, ok := game.Players[playerName]; !ok {
		log.Printf(fmt.Sprintf("Adding player %s to %p", playerName, game))
		game.Players[playerName] = &PlayerMetadata{JoinTime: time.Now()}
	}
}

func (game *Game) BuildPlayerList() string {
	var sortedPlayers []Player
	for key, value := range game.Players {
		sortedPlayers = append(sortedPlayers, Player{key, *value})
	}

	sort.Slice(sortedPlayers, func(i, j int) bool {
		return sortedPlayers[i].Value.JoinTime.Before(sortedPlayers[j].Value.JoinTime)
	})

	var sortedPlayerNames []string
	for _, player := range sortedPlayers {
		message := player.Key
		if player.Value.PickOrder != 0 {
			message = fmt.Sprintf("**%d)** %s", player.Value.PickOrder, player.Key)
		}
		sortedPlayerNames = append(sortedPlayerNames, message)
	}

	return strings.Join(sortedPlayerNames, " :small_orange_diamond: ")
}

func (game *Game) RandPlayer() string {
	keys := reflect.ValueOf(game.Players).MapKeys()
	return keys[rand.Intn(len(keys))].String()
}

func (game *Game) BeginPicks(s *discordgo.Session, channelID string, modName string, mod *Mod) {
	// TODO: Mention both players if mod has maxPlayers of 2
	// TODO: Configurable countdown
	var seconds int = 20
	messageText := fmt.Sprintf("**%s** has filled.\nCaptains will be selected in `%d seconds`", modName, seconds)
	message, error := s.ChannelMessageSend(channelID, messageText)

	if error != nil {
		return
	}

	countdownTicker := time.NewTicker(time.Second)
	go func() {
		for range countdownTicker.C {
			seconds -= 1
			log.Printf(fmt.Sprintf("Ticking %d for %p", seconds, game))

			if !game.IsFull(mod) {
				s.ChannelMessageEdit(channelID, message.ID, fmt.Sprintf("**%s** has filled.\n~~Captains will be selected in `%d seconds`~~", modName, seconds))
				countdownTicker.Stop()
			} else if seconds <= 0 && !game.IsPickingTeams(mod) {
				messageText := fmt.Sprintf("**%s** has filled.\nCaptains have been selected", modName)
				s.ChannelMessageEdit(channelID, message.ID, messageText)
				countdownTicker.Stop()
				game.AutoPickRemainingCaptains(s, channelID)
			} else if game.IsFull(mod) && seconds > 0 && (seconds%5 == 0 || seconds < 5) {
				messageText := fmt.Sprintf("**%s** has filled.\nCaptains will be selected in `%d seconds`", modName, seconds)
				s.ChannelMessageEdit(channelID, message.ID, messageText)
			} else if game.IsPickingTeams(mod) {
				countdownTicker.Stop()
			}
		}
	}()
}

func (game *Game) AutoPickRemainingCaptains(s *discordgo.Session, channelID string) {
	var message []string
	for x := 0; x < 2; x++ {
		captainMessage := game.SetNextCaptainIfPossible(game.RandPlayer())
		if captainMessage != "" {
			message = append(message, captainMessage)
		}
	}
	message = append(message, fmt.Sprintf("%s to pick", *game.RedCaptain))

	s.ChannelMessageSend(channelID, strings.Join(message, "\n"))
	game.establishPickOrder()
	s.ChannelMessageSend(channelID, game.BuildPlayerList())
}

func (game *Game) SetCaptain(captain string, teamCaptain **string, team *map[string]bool) {
	delete(game.Players, captain)
	*teamCaptain = &captain
	(*team)[captain] = true
}

// Sets a captain to `userName` if there is room for them. Returns a message to send to a channel if so
func (game *Game) SetNextCaptainIfPossible(userName string) string {
	var teamName string
	if *game.RedCaptain == "" && *game.BlueCaptain != userName {
		game.SetCaptain(userName, &game.RedCaptain, &game.Red)
		teamName = "Red"
	} else if *game.BlueCaptain == "" && *game.RedCaptain != userName {
		game.SetCaptain(userName, &game.BlueCaptain, &game.Blue)
		teamName = "Blue"
	}

	if teamName != "" {
		return fmt.Sprintf("%s is captain for the **%s Team**", userName, teamName)
	}

	return ""
}

func (game *Game) PickColor() TeamColor {
	pickedCount := len(game.Red) + len(game.Blue)
	return pickedCount > 0 && ((pickedCount-1)/2)%2 == 1
}

func (game *Game) NameByPickOrder(i int) string {
	for name, player := range game.Players {
		if player.PickOrder == i {
			return name
		}
	}
	return ""
}

// internal

func (game *Game) establishPickOrder() {
	i := 1
	for _, player := range game.Players {
		player.PickOrder = i
		i++
	}
}
