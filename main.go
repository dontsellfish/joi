package main

import (
	"encoding/json"
	"flag"
	tele "gopkg.in/telebot.v3"
	"joi2/joi"
	"log"
	"os"
	"time"
)

const DefaultConfigPath = "./cfg.json"

func main() {
	cfgPath := flag.String("cfg", DefaultConfigPath, "path to a config file")
	isVerbose := flag.Bool("verbose", false, "run tg bot in verbose mode")
	flag.Parse()

	buff, err := os.ReadFile(*cfgPath)
	if err != nil {
		log.Fatalln(err.Error())
	}

	var cfg joi.Config
	err = json.Unmarshal(buff, &cfg)
	if err != nil {
		log.Fatalln(err.Error())
	}

	bot, err := joi.NewJoi(cfg, tele.Settings{
		Token:       cfg.Token,
		Poller:      &tele.LongPoller{Timeout: time.Second * 60},
		Synchronous: true,
		Verbose:     *isVerbose,
		ParseMode:   "",
		OnError:     nil,
	})
	if err != nil {
		log.Fatalln(err.Error())
	}

	bot.Start()
}
