package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"cloud.google.com/go/firestore"
	"github.com/bwmarrin/discordgo"
	"github.com/google/logger"
	"google.golang.org/api/iterator"
)

// Variables used for command line parameters
var (
	Token string
	bot   Bot
)

const logPath = "bot.log"

var verbose = flag.Bool("verbose", false, "print info level logs to stdout")

func init() {

	flag.StringVar(&Token, "t", "", "Bot Token")
	flag.Parse()
}

func main() {
	lf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0660)
	if err != nil {
		logger.Fatalf("Failed to open log file: %v", err)
	}
	fmt.Println(*lf)
	defer lf.Close()
	defer logger.Init("LoggerExample", false, false, lf).Close()

	// Create a new Discord session using the provided bot token.
	token := os.Getenv("TOKEN")
	if token == "" {
		log.Printf("TOKEN: %s", token)
		token = Token
	}
	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		logger.Fatalf("error creating Discord session,", err)
		return
	}
	botname := os.Getenv("BOTNAME")
	if botname == "" {
		botname = "discord-pugbot"
	}

	// Get a Firestore client.
	ctx := context.Background()
	client := createClient(ctx, botname)
	defer client.Close()

	channels := make(map[string]Channel)
	games := make(map[GameIdentifier]Game)

	iter := client.Collection("channels").Documents(ctx)
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Fatalf("Failed to iterate: %v", err)
		}
		var c Channel
		doc.DataTo(&c)
		channels[doc.Ref.ID] = c
		for name := range c.Mods {
			g := GameIdentifier{doc.Ref.ID, name}
			games[g] = Game{Players: make(map[string]PlayerMetadata), Red: make(map[string]bool), Blue: make(map[string]bool), mutex: new(sync.Mutex), RedCaptain: new(string), BlueCaptain: new(string)}
		}
	}

	bot = Bot{channels, games, client, ctx}

	// Register the messageCreate func as a callback for MessageCreate events.
	dg.AddHandler(messageCreate)

	// Open a websocket connection to Discord and begin listening.
	err = dg.Open()
	if err != nil {
		logger.Fatalf("error opening connection,", err)
		return
	}

	// Wait here until CTRL-C or other term signal is received.
	log.Println("Bot is now running. Press CTRL-C to exit.")
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
		log.Printf("defaulting to port %s", port)
	}
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
	http.HandleFunc("/", handler)
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt, os.Kill)
	<-sc
	// Cleanly close down the Discord session.
	dg.Close()
}

// Converts input string to a reflection-compatible corresponding method string, e.g.
// .foo bar -> Foo
func parseCommand(s string) string {
	com := strings.Split(strings.TrimLeft(s, "."), " ")
	if len(com) > 0 {
		return strings.Title(com[0])
	}
	return ""
}

// Returns all command arguments, i.e. all words except from the first one.
// If the arguments include brackets, consider them as one arguments, e.g.
// .foo bar "hello world" -> ["bar", "hello world"]
func parseArguments(s string) []string {
	re := regexp.MustCompile(`[^\s"']+|([^\s"']*"([^"]*)"[^\s"']*)+|'([^']*)`)
	args := re.FindAllString(s, -1)
	if len(args) == 0 {
		return []string{}
	}
	return args[1:]
}

func messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	logger.Info(m.Content)
	args := parseArguments(m.Content)
	// Pass the original session and message arguments.
	inputs := make([]reflect.Value, len(args)+2)
	inputs[0] = reflect.ValueOf(s)
	inputs[1] = reflect.ValueOf(m)
	// Pass any additional arguments based on the message itself.
	for i := range args {
		val := args[i]
		if valInt, err := strconv.Atoi(val); err == nil {
			inputs[i+2] = reflect.ValueOf(valInt)
		} else {
			inputs[i+2] = reflect.ValueOf(val)
		}
	}

	// Ignore all messages created by the bot itself
	// This isn't required in this specific example but it's a good practice.
	if m.Author.ID == s.State.User.ID {
		return
	}
	method := reflect.ValueOf(&bot).MethodByName(parseCommand(m.Content))
	if method.IsValid() && len(inputs) >= method.Type().NumIn() {
		// Trim all unnecessary arguments.
		inputs = inputs[:method.Type().NumIn()]
		method.Call(inputs)
	}
}

func handler(w http.ResponseWriter, r *http.Request) {
	name := os.Getenv("NAME")
	if name == "" {
		name = "World"
	}
	fmt.Fprintf(w, "Hello %s!\n", name)
}

func createClient(ctx context.Context, token string) *firestore.Client {
	client, err := firestore.NewClient(ctx, token)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	// Close client when done with
	// defer client.Close()
	return client
}
