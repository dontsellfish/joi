## Joi.

> Rethought and extended Telegram scheduled messages.

Problems with Telegram scheduled messages implementation:

1. Sucks, very restrictive (only 100 media files),
   you can't schedule posts with tags, not making your channel looking ugly
2. Telegram compresses images to Jpeg by `convert -quality 87 -reisze 1280x1280`, which reaps the soul of any picture.
   That could be avoided (sort of) by posting via unofficial clients, which I can't recommend, or
   via posting with Bots

### Features

1. Basically unlimited media/posts count
2. Pictures resolutions are up to 4K
3. No need to specify each post individually, if there's an established schedule,
   i.e. you post every day at 06:00, 12:00 and etc
4. Schedule comments to be released posts, to put there tags/sources, any other commentaries
5. Sure thing, you could run multiple instances of bots at the same time.

(If there's a post, which you what to post specifically on some day, you still could do it.)

### Usage guide

One day I'll write little article about it, but not today. Run a bot, and I'm sure, you'll figure this out.
I think my description of the commands is pretty self-explanatory.

### Install & Run

```sheell
git clone https://github.com/dontsellfish/joi
cd joi
go build
```

- ```./joi``` — to run using `cfg.json` as the config
- ```./joi -cfg example.json -verbose``` — that's all command line options

### Configuration

Requires ids of the channel, comments, admins, telegram bot token.
Check `example.json` or `exampleextended.json` for more details.

### Limits and possible caveates

Media have to be lesser than 20MB, that's Telegram's restriction for bots.
This could be solved by using user-bots, but that's a very quirky path, and I'm not sure that I want to follow it.