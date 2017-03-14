package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/davidjpeacock/shelbot/irc"
)

const version = "2.0.0"

var homeDir string

func init() {
	usr, err := user.Current()
	if err != nil {
		log.Fatal(err)
	}
	homeDir = usr.HomeDir
}

type Pair struct {
	Key   string
	Value int
}

func main() {
	var bot *config
	var conn *irc.Conn
	var k *karma
	var err error
	var karmaFunc func(string) int

	logFile, err := os.OpenFile(filepath.Join(homeDir, ".shelbot.log"), os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		log.Fatal(err)
	}
	log.SetOutput(logFile)

	confFile := flag.String("config", filepath.Join(homeDir, ".shelbot.conf"), "config file to be used with shelbot")
	karmaFile := flag.String("karmaFile", filepath.Join(homeDir, ".shelbot.json"), "karma db file")
	v := flag.Bool("v", false, "Prints Shelbot version")
	flag.Parse()

	if *v {
		fmt.Println("Shelbot version " + version)
		return
	}

	if bot, err = loadConfig(*confFile); err != nil {
		log.Fatalf("Error reading config file: %s", err)
	}

	if k, err = readKarmaFileJSON(*karmaFile); err != nil {
		log.Fatalf("Error loading karma DB: %s", err)
	}

	conn = irc.New(bot.Server, bot.Port, bot.Nick, bot.User)
	irc.Debug.SetOutput(logFile)

	if err = conn.Connect(); err != nil {
		log.Fatal(err)
	}

	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)
		<-c
		log.Println("Received SIGTERM, exiting")
		conn.Quit("Bazinga!")
		if err = k.save(); err == nil {
			if f, ok := k.dbFile.(*os.File); ok {
				f.Close()
			}
		}
		logFile.Close()
		os.Exit(0)
	}()

	conn.Join(bot.Channel, "")
	conn.PrivMsg(bot.Channel, fmt.Sprintf("%s version %s reporting for duty", bot.Nick, version))

	go conn.Listen()

	for msg := range conn.PrivMessages {
		lineElements := strings.Fields(msg.Text)

		if lineElements[0] == bot.Nick {
			if lineElements[1] == "help" {
				conn.PrivMsg(bot.Channel, fmt.Sprintf("%s commands available: \"help\", \"version\", \"query item\", \"topten\", \"bottomten\"", bot.Nick))
				conn.PrivMsg(bot.Channel, "Karma can be adjusted thusly: \"foo++\" and \"bar--\"")
				log.Println("Shelbot help provided.")
			}

			if lineElements[1] == "version" {
				conn.PrivMsg(bot.Channel, fmt.Sprintf("%s version %s.", bot.Nick, version))
				log.Println("Shelbot version " + version)
			}

			if lineElements[1] == "query" && len(lineElements) > 2 {
				for _, q := range lineElements[2:] {
					karmaValue := k.query(q)
					response := fmt.Sprintf("Karma for %s is %d.", q, karmaValue)
					conn.PrivMsg(bot.Channel, response)
					log.Println(response)
				}
			}

			if lineElements[1] == "topten" || lineElements[1] == "bottomten" {
				var p []Pair
				for k, v := range k.db {
					p = append(p, Pair{k, v})
				}

				switch lineElements[1] {
				case "topten":
					sort.Slice(p, func(i, j int) bool { return p[i].Value > p[j].Value })
				case "bottomten":
					sort.Slice(p, func(i, j int) bool { return p[i].Value < p[j].Value })
				}

				for i := 0; i < 10 && i < len(p); i++ {
					response := fmt.Sprintf("Karma for %s is %d.", p[i].Key, p[i].Value)
					conn.PrivMsg(bot.Channel, response)
					log.Println(response)
				}
			}
			continue
		}

		if !strings.HasSuffix(msg.Text, "++") && !strings.HasSuffix(msg.Text, "--") {
			continue
		}

		var handle = strings.Trim(lineElements[len(lineElements)-1], "+-")

		if strings.HasSuffix(msg.Text, "++") {
			karmaFunc = k.increment
		} else if strings.HasSuffix(msg.Text, "--") {
			karmaFunc = k.decrement
		}

		karmaTotal := karmaFunc(handle)
		response := fmt.Sprintf("Karma for %s now %d", handle, karmaTotal)
		conn.PrivMsg(bot.Channel, response)
		log.Println(response)

		if err = k.save(); err != nil {
			log.Fatalf("Error saving karma db: %s", err)
		}
	}
}
