package main

import (
	"math/rand"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
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
	return game.IsFull(mod) && game.RedCaptain != nil && game.BlueCaptain != nil
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
		sortedPlayerNames = append(sortedPlayerNames, player.Key)
	}

	return strings.Join(sortedPlayerNames, " | ")
}

func (game *Game) RandPlayer() string {
	keys := reflect.ValueOf(game.Players).MapKeys()
	return keys[rand.Intn(len(keys))].String()
}
