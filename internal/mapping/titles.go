package mapping

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed titles.json
var titlesJson []byte
var titles map[string]string

type gameTitle struct {
	Title   string `json:"title"`
	TitleID string `json:"titleid"`
}

func loadTitles() {
	titles = map[string]string{}

	var ts []gameTitle
	if err := json.Unmarshal(titlesJson, &ts); err != nil {
		fmt.Println("Error decoding game titles, displaying IDs")
	}
	for _, t := range ts {
		titles[t.TitleID] = t.Title
	}
}

func Title(game string) string {
	if titles == nil {
		loadTitles()
	}
	if title, ok := titles[game]; ok {
		return title
	}
	return game
}
