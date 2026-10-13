package mapping

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

//go:embed titles.json
var titlesJson []byte
var titles map[string]string
var once sync.Once

type gameTitle struct {
	Title   string `json:"title"`
	TitleID string `json:"titleid"`
}

func loadTitles() {
	titles = make(map[string]string)

	var ts []gameTitle
	if err := json.Unmarshal(titlesJson, &ts); err != nil {
		fmt.Println("Error decoding game titles, displaying IDs")
		return
	}
	for _, t := range ts {
		titles[strings.ToUpper(t.TitleID)] = t.Title
	}
}

func Title(game string) string {
	once.Do(loadTitles)
	if title, ok := titles[strings.ToUpper(game)]; ok {
		return fmt.Sprintf("%s (%s)", title, strings.ToUpper(game))
	}
	return game
}
