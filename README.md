# discord-pugbot
> A pug bot for Discord

## Design

In order to add a command, you simply have to add a public method on a `Bot` type. Function name and arguments will be automatically mapped to a command, e.g. `func (b Bot) Randomquote(s *discordgo.Session, m *discordgo.MessageCreate, user string)` will be mapped to `.randomquote user`. 

## Running

1. **Make sure to get Go 1.7.3 or higher**

2. **Install dependencies and build the bot**

`go get` & `go build`

3. **Run the bot**

`./discord-pugbot -t <bot token>`

## Usage
Commands for this bot follow this structure: `.<command> [argument1] [argument2]`.

| Command | Description
|---------|-------------|
| `.randomquote user` | Shows random quote for particular user. |
| `.lastquote user` | Shows last quote for particular user. |

To add a quote, you simply have to react with ❤️ to any of their messages.