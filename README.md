# discord-pugbot
> A pug bot for Discord

## Design

In order to add a command, you simply have to add a public method on a `Bot` type. Function name and arguments will be automatically mapped to a command, e.g. `func (b Bot) Randomquote(s *discordgo.Session, m *discordgo.MessageCreate, user string)` will be mapped to `.randomquote user`. 

## Running

1. **Make sure to get Go 1.7.3 or higher**

2. **Install dependencies and build the bot**

`go get` & `go build`

3. **Install google cloud SDK**

https://cloud.google.com/sdk/docs/install

4. **Start local firestore instance**

Run:

`gcloud beta emulators firestore start --host-port=localhost`

You will see something like:

`[firestore] API endpoint: http://localhost:8080`

This is your local firestore endpoint.

5. **Run the bot**

`./discord-pugbot -t <bot token> -l <firestore endpoint>`

Use firestore endpoint from the step above.

## Usage
Commands for this bot follow this structure: `.<command> [argument1] [argument2]`.

| Command | Description
|---------|-------------|
| `.lsa` | Shows all active mods and added players. |
| `.ls [mod]` | Shows players who joined particular mod. |
| `.join [mod]` | Joins a particular mod. |
| `.j [mod]` | Joins a particular mod. |
