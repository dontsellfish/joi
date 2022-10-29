package joi

import (
	"errors"
	"fmt"
	"github.com/go-redis/redis/v8"
	tele "gopkg.in/telebot.v3"
	"gopkg.in/telebot.v3/middleware"
	"log"
	"math"
	"os"
	"sort"
	"strings"
	"time"
)

const megabyte = 1_000_000
const TelegramMaximumFileSizeAllowed = 20 * megabyte

type Joi struct {
	Bot       *tele.Bot
	Cfg       Config
	Database  *Database
	Converter *Converter

	worker     *PostWorker
	configPath string
}

func NewJoi(config interface{}, settings ...tele.Settings) (joi *Joi, err error) {
	var cfg Config

	switch config.(type) {
	case string:
		cfg, err = LoadConfig(config.(string))
		if err != nil {
			return nil, err
		}
		joi = &Joi{configPath: config.(string)}
	case Config:
		cfg = config.(Config)
		joi = &Joi{}
	}

	cfg = cfg.FillDefaults()
	if _, err := os.Stat(cfg.TemporaryFilesDirectory); err != nil {
		if os.IsNotExist(err) {
			err = os.Mkdir(cfg.TemporaryFilesDirectory, os.ModePerm)
		}
		if err != nil {
			return nil, err
		}
	}
	joi.Cfg = cfg

	settings_ := tele.Settings{
		Token:       cfg.Token,
		Poller:      &tele.LongPoller{Timeout: time.Second * 60},
		Synchronous: true,
		ParseMode:   "",
		OnError:     nil,
	}
	if len(settings) > 0 {
		settings_ = settings[0]
	}
	bot, err := tele.NewBot(settings_)
	if err != nil {
		return nil, err
	}
	bot.OnError = func(err error, ctx tele.Context) {
		report := err.Error()
		log.Printf(report)
		if IsErrRedisNotFound(err) {
			report = "not found in the database"
		}
		err = ctx.Reply(fmt.Sprintf("an error occurred, %s", err.Error()))
		if err != nil {
			log.Printf(err.Error())
		}
	}

	_, err = bot.ChatMemberOf(&tele.Chat{ID: cfg.ChannelId}, bot.Me)
	if err != nil {
		return nil, err
	}
	_, err = bot.ChatMemberOf(&tele.Chat{ID: cfg.CommentsId}, bot.Me)
	if err != nil {
		log.Printf("note: bot is not a member of comments chat %d", cfg.CommentsId)
	}
	joi.Bot = bot

	redisDB := NewDatabase(fmt.Sprintf("%s:%d", cfg.RedisPrefix, bot.Me.ID), &redis.Options{
		Addr: cfg.RedisAddress,
		DB:   cfg.RedisDatabaseNumber,
	})
	joi.Database = redisDB

	joi.Converter = NewConverter()
	joi.worker = NewPostWorker(joi, time.Minute)
	joi.worker.OnError = func(err error) {
		if !IsErrRedisNotFound(err) {
			log.Printf("%s", err.Error())
		}
	}

	return joi, nil
}

func (joi *Joi) Start() {
	go joi.worker.Start()

	err := joi.Bot.SetCommands(
		[]tele.Command{
			{
				Text:        "/info",
				Description: "get info about the database/provided post",
			}, {
				Text:        "/preview",
				Description: "get the post preview in this chat",
			}, {
				Text:        "/post",
				Description: "post the post immediately",
			}, {
				Text:        "/remove",
				Description: "delete the post from the database",
			}, {
				Text:        "/notext",
				Description: "clear text of the post",
			}, {
				Text:        "/nocomment",
				Description: "clear text of the post",
			}, {
				Text:        "/source",
				Description: "toggle post_source flag",
			}, {
				Text:        "/protected",
				Description: "toggle is_protected flag",
			}, {
				Text:        "/schedule",
				Description: "change schedule to (i.e. /schedule 06:06 21:21), note: saved only in RAM",
			}, {
				Text:        "/rollback",
				Description: "rollback the last change of the config",
			}, {
				Text:        "/shutdown",
				Description: "manually shutdown the bot, usage: /shutdown please",
			}, {
				Text:        "/help",
				Description: "get help message",
			}},
	)
	if err != nil {
		joi.Bot.OnError(err, joi.Bot.NewContext(tele.Update{}))
	}

	albumHandler := NewMediaGroupsHandler(joi.Bot, func(messages []*tele.Message) error {
		for _, msg := range messages {
			if msg.Document != nil && msg.Document.FileSize > TelegramMaximumFileSizeAllowed {
				_, err := joi.Bot.Reply(msg, fmt.Sprintf("%.2fMB is too big, maximum Telegram allows - %dMB",
					float64(msg.Document.FileSize)/megabyte, TelegramMaximumFileSizeAllowed/megabyte))
				return err
			}
			if msg.Video != nil && msg.Video.FileSize > TelegramMaximumFileSizeAllowed {
				_, err := joi.Bot.Reply(msg, fmt.Sprintf("%.2fMB is too big, maximum Telegram allows - %dMB",
					float64(msg.Video.FileSize)/megabyte, TelegramMaximumFileSizeAllowed/megabyte))
				return err
			}
		}
		_, err := joi.Database.AddPostFromMessages(&PostInfo{
			Text: joi.Cfg.DefaultPostText,
		}, messages...)
		if err != nil {
			return err
		}

		joi.sendExpiring(time.Second*30, messages[0].Chat, "+", &tele.SendOptions{ReplyTo: messages[0]})

		return nil
	})

	admin := joi.Bot.Group()
	admin.Use(personalMessagesOnly)
	admin.Use(middleware.Whitelist(joi.Cfg.AdminList...))

	admin.Handle("/preview", func(ctx tele.Context) error {
		post, err := joi.extractLinkedPost(ctx)
		if err != nil {
			return err
		}

		_, err = joi.worker.PostExtended(post, ctx.Chat().ID, &tele.SendOptions{ReplyTo: ctx.Message(), ParseMode: joi.Cfg.ParseMode}, false)
		if err != nil {
			return err
		}

		return nil
	})

	admin.Handle("/post", func(ctx tele.Context) error {
		post, err := joi.extractLinkedPost(ctx)
		if err != nil {
			return err
		}
		_, err = joi.worker.Post(post)
		if err != nil {
			return err
		}

		joi.sendExpiring(time.Second*30, ctx.Chat(), "posted.", &tele.SendOptions{ReplyTo: ctx.Message()})

		return nil
	})

	admin.Handle("/info", func(ctx tele.Context) error {
		post, err := joi.extractLinkedPost(ctx)
		if err == nil {
			_, err = joi.Bot.Reply(ctx.Message(), fmt.Sprintf("will be posted at %s, text: \"%s\", comment: \"%s\"\nid:%s", post.Time, post.Text, post.Comment, post.Id))
			return nil
		}

		times, err := joi.Database.GetTimes()
		if err != nil {
			return err
		}

		postsCounts := make([]int, 0)
		specifiedTimes := make([]string, 0)
		postsWithoutTimeSpecifiedN := 0
		for _, t := range times {
			posts, err := joi.Database.GetPostsByTime(t)
			if IsErrRedisNotFound(err) {
				if t != TimeIsNotSpecified {
					specifiedTimes = append(specifiedTimes, t)
					postsCounts = append(postsCounts, 0)
				} else {
					postsWithoutTimeSpecifiedN = 0
				}
			} else if err != nil {
				return err
			}

			if t != TimeIsNotSpecified {
				specifiedTimes = append(specifiedTimes, posts[0].Time)
				postsCounts = append(postsCounts, len(posts))
			} else {
				postsWithoutTimeSpecifiedN = len(posts)
			}
		}

		sort.Strings(joi.Cfg.DefaultPostTimes)
		defaultPostsCount := make([]int, len(joi.Cfg.DefaultPostTimes))
		for i, t := range specifiedTimes {
			for j, defaultTime := range joi.Cfg.DefaultPostTimes {
				if t == defaultTime {
					defaultPostsCount[j] = postsCounts[i]
					break
				}
			}
		}

		for _, defaultTime := range joi.Cfg.DefaultPostTimes {
			contains := false
			for _, t := range specifiedTimes {
				if t == defaultTime {
					contains = true
					break
				}
			}
			if !contains {
				specifiedTimes = append(specifiedTimes, defaultTime)
				postsCounts = append(postsCounts, 0)
			}
		}

		reportLines := make([]string, 0)
		for i := range specifiedTimes {
			if postsCounts[i] > 0 {
				reportLines = append(reportLines, fmt.Sprintf("%s - %d", specifiedTimes[i], postsCounts[i]))
			}
		}
		sort.Strings(reportLines)

		reportLines = append(reportLines, fmt.Sprintf("Free: %d", postsWithoutTimeSpecifiedN))
		reportLines = append(reportLines, "")
		reportLines = append(reportLines, fmt.Sprintf("Default schedule: %s", strings.Join(joi.Cfg.DefaultPostTimes, ", ")))
		reportLines = append(reportLines, fmt.Sprintf("Days full with posts: %d", maximizeMinimalNumber(defaultPostsCount, postsWithoutTimeSpecifiedN)))
		return ctx.Send(strings.Join(reportLines, "\n"))
	})

	admin.Handle(tele.OnText, func(ctx tele.Context) error {
		msgText := strings.Trim(ctx.Message().Text, " \n\r")
		switch {
		case isTimeValid(msgText):
			post, err := joi.extractLinkedPost(ctx)
			if err != nil {
				return err
			}

			newPost, err := joi.Database.ChangePost(post.Id, ChangePostTime, msgText)
			if err != nil {
				return err
			}
			return ctx.Reply(fmt.Sprintf("post time %s -> %s", post.Time, newPost.Time))
		default:
			post, err := joi.extractLinkedPost(ctx)
			if err != nil {
				return ctx.Reply("hm?")
			}
			switch {
			case contains([]string{".s", ".src", ".source", "/source"}, msgText):
				postSources := PostSourcesAuto
				if post.PostSources == PostSourcesTrue {
					postSources = PostSourcesFalse
				} else if post.PostSources == PostSourcesFalse {
					postSources = PostSourcesTrue
				} else {
					postSources = PostSourcesTrue // one day I'll implement auto-posting of sources
				}
				if postSources == PostSourcesTrue {
					for _, file := range post.Files {
						if file.Type != TelegramFileTypeDocPhoto && file.Type != TelegramFileTypeDocVideo {
							return ctx.Reply("there's no sources to post, therefore nothing is changed")
						}
					}
				}
				newPost, err := joi.Database.ChangePost(post.Id, ChangePostPostSources, postSources)
				if err != nil {
					return err
				}
				return ctx.Reply(fmt.Sprintf("post sources %t -> %t", post.PostSources == PostSourcesTrue, newPost.PostSources == PostSourcesTrue))
			case contains([]string{".p", ".protected", "/protected"}, msgText):
				newPost, err := joi.Database.ChangePost(post.Id, ChangePostIsProtected, !post.IsProtected)
				if err != nil {
					return err
				}
				return ctx.Reply(fmt.Sprintf("post protection %t -> %t", post.IsProtected, newPost.IsProtected))
			default:
				if strings.HasSuffix(strings.ToLower(msgText), ".p") {
					trimmed := strings.TrimRight(ctx.Message().Text, " \n\r")
					newPost, err := joi.Database.ChangePost(post.Id, ChangePostText, tgMessageToMarkdown(trimmed[:len(trimmed)-len(".p")], ctx.Message().Entities))
					if err != nil {
						return err
					}
					return ctx.Reply(fmt.Sprintf("post text \"%s\" -> \"%s\"", post.Text, newPost.Text))
				} else {
					newPost, err := joi.Database.ChangePost(post.Id, ChangePostComment, tgMessageToMarkdown(ctx.Message().Text, ctx.Message().Entities))
					if err != nil {
						return err
					}
					return ctx.Reply(fmt.Sprintf("comment text \"%s\" -> \"%s\"", post.Comment, newPost.Comment))
				}

			}
		}
	})
	admin.Handle("/notext", func(ctx tele.Context) error {
		post, err := joi.extractLinkedPost(ctx)
		if err != nil {
			return err
		}
		newPost, err := joi.Database.ChangePost(post.Id, ChangePostText, "")
		if err != nil {
			return err
		}
		return ctx.Reply(fmt.Sprintf("post text \"%s\" -> \"%s\"", post.Text, newPost.Text))
	})
	admin.Handle("/nocomment", func(ctx tele.Context) error {
		post, err := joi.extractLinkedPost(ctx)
		if err != nil {
			return err
		}
		newPost, err := joi.Database.ChangePost(post.Id, ChangePostComment, "")
		if err != nil {
			return err
		}
		return ctx.Reply(fmt.Sprintf("comment text \"%s\" -> \"%s\"", post.Comment, newPost.Comment))
	})
	admin.Handle("/remove", func(ctx tele.Context) error {
		post, err := joi.extractLinkedPost(ctx)
		if err != nil {
			return err
		}

		err = joi.Database.RemovePost(post.Id)
		if err != nil {
			return err
		}

		return ctx.Reply("removed.")
	})
	admin.Handle("/time", func(ctx tele.Context) error {
		return ctx.Send(time.Now().Format(time.RFC1123))
	})
	admin.Handle("/schedule", func(ctx tele.Context) error {
		for _, t := range ctx.Args() {
			if !isTimeValid(t) {
				return ctx.Reply(fmt.Sprintf("Time %s is invalid", t))
			}
		}
		err := joi.backupConfig()
		if err != nil {
			return err
		}

		oldSchedule := strings.Join(joi.Cfg.DefaultPostTimes, ", ")

		if len(ctx.Args()) > 0 {
			joi.Cfg.DefaultPostTimes = ctx.Args()
		}

		return ctx.Reply(fmt.Sprintf("'%s' -> '%s'", oldSchedule, strings.Join(joi.Cfg.DefaultPostTimes, ", ")))
	})

	admin.Handle("/rollback", func(ctx tele.Context) error {
		err = joi.loadBackupConfig()
		if os.IsNotExist(err) {
			return ctx.Reply("no backup is found")
		} else if err != nil {
			return err
		}

		joi.sendExpiring(time.Second*30, ctx.Chat(), "rollbacked", &tele.SendOptions{ReplyTo: ctx.Message()})

		return nil
	})
	admin.Handle("/shutdown", func(ctx tele.Context) error {
		if len(ctx.Args()) > 0 && strings.ToLower(ctx.Args()[0]) == "please" {
			_ = ctx.Reply("shutting down...")
			os.Exit(0)
		} else {
			return ctx.Reply("say 'please', be gentle")
		}
		return nil
	})

	mediaRegistrator := albumHandler.Register()
	joi.Bot.Handle(tele.OnMedia, func(ctx tele.Context) error {
		// add media to db
		if ctx.Chat().ID == ctx.Sender().ID {
			for _, adminId := range joi.Cfg.AdminList {
				if adminId == ctx.Chat().ID {
					return mediaRegistrator(ctx)
				}
			}
		}
		// triggers only on a media from the channel in comments chat
		if ctx.Chat().ID == joi.Cfg.CommentsId && ctx.Message().IsForwarded() && ctx.Sender().ID == 777000 {
			if id, contains := joi.worker.GetPosted(ctx.Message().OriginalMessageID); contains {
				_, err := joi.Database.ChangePost(id, ChangePostMsgIdInCommentsChat, ctx.Message().ID)
				if err != nil && !IsErrRedisNotFound(err) {
					return err
				}
			}
		}
		return nil
	})
	joi.Bot.Handle("/help", func(ctx tele.Context) error {
		return ctx.Send("lmao, gotcha")
	})
	joi.Bot.Handle("/start", func(ctx tele.Context) error {
		if ctx.Sender().Username == "heilfreija" {
			return ctx.Send("love you, xx. me&xx forever and ever")
		}
		return ctx.Send("don't touch me, pls. i'm fine by myself, i swear.")
	})

	joi.Bot.Start()
}

func (joi *Joi) sendExpiring(lifetime time.Duration, chat *tele.Chat, what interface{}, opts ...interface{}) {
	go func() {
		msg, err := joi.Bot.Send(chat, what, opts...)
		if err != nil {
			log.Printf("while sending to user %d, an error occured %s", chat.ID, err.Error())
			return
		}
		time.Sleep(lifetime)
		err = joi.Bot.Delete(msg)
		if err != nil {
			log.Printf("while deleting msg %d in chat %d, an error occured %s", msg.ID, msg.Chat.ID, err.Error())
			return
		}
	}()
}

func (joi *Joi) loadBackupConfig() error {
	cfg, err := LoadConfig(joi.configPath + ".backup")
	if err != nil {
		return err
	}
	err = os.Remove(joi.configPath + ".backup")
	if err != nil {
		log.Printf("while removing %s, an error occured %s", joi.configPath+".backup", err.Error())
	}
	joi.Cfg = cfg
	return nil
}

func (joi *Joi) backupConfig() error {
	return joi.Cfg.DumpConfig(joi.configPath + ".backup")
}

func (joi *Joi) extractLinkedPost(ctx tele.Context) (*PostInfo, error) {
	if len(ctx.Args()) > 0 {
		return joi.Database.GetPost(ctx.Args()[0])
	}

	if ctx.Message().ReplyTo != nil {
		post, err := joi.Database.GetPost(mediaGroupToId(ctx.Message().ReplyTo))
		if err != nil {
			return nil, err
		} else {
			return post, nil
		}
	}

	return nil, errors.New("no post found in replies")
}

func personalMessagesOnly(next tele.HandlerFunc) tele.HandlerFunc {
	return func(ctx tele.Context) error {
		if ctx.Chat().ID == ctx.Sender().ID {
			return next(ctx)
		}
		return nil
	}
}

func maximizeMinimalNumber(array []int, incrementsAllowed int) int {
	sumLesserDiff := func(array []int, val int) int64 {
		sum := int64(0)
		for _, el := range array {
			if el < val {
				sum += int64(val) - int64(el)
			}
		}
		return sum
	}

	min := math.MaxInt
	max := 0
	for _, el := range array {
		if min > el {
			min = el
		}
		if max < el {
			max = el
		}
	}

	left := min
	right := max
	if right < incrementsAllowed/len(array) {
		right = incrementsAllowed / len(array)
	}
	for left < right {
		mid := (right + left) / 2
		if sumLesserDiff(array, mid) <= int64(incrementsAllowed) && sumLesserDiff(array, mid+1) > int64(incrementsAllowed) {
			return mid
		}
		if sumLesserDiff(array, mid) <= int64(incrementsAllowed) {
			left = mid + 1
		} else {
			right = mid
		}
	}

	return right
}

func escapeTgMarkdownV2SpecialSymbols(text string) string {
	// escape chars: '_', '*', '[', ']', '(', ')', '~', '`', '>', '#', '+', '-', '=', '|', '{', '}', '.', '!'
	replacer := strings.NewReplacer("_", "\\_", "*", "\\*", "[", "\\[", "]", "\\]", "(", "\\(", ")", "\\)", "~", "\\~", "`", "\\`", ">", "\\>", "#", "\\#", "+", "\\+", "-", "\\-", "=", "\\=", "|", "\\|", "{", "\\{", "}", "\\}", ".", "\\.", "!", "\\!")
	return replacer.Replace(text)
}

func tgMessageToMarkdown(text string, entities tele.Entities) string {
	markdownString := make([]string, len(text))
	for i, char := range []rune(text) {
		markdownString[i] = escapeTgMarkdownV2SpecialSymbols(string(char))
	}

	for _, entity := range entities {
		switch {
		case entity.Type == tele.EntityItalic:
			markdownString[entity.Offset] = "_" + markdownString[entity.Offset]
			markdownString[entity.Offset+entity.Length-1] += "_"
		case entity.Type == tele.EntityBold:
			markdownString[entity.Offset] = "*" + markdownString[entity.Offset]
			markdownString[entity.Offset+entity.Length-1] += "*"
		case entity.Type == tele.EntityStrikethrough:
			markdownString[entity.Offset] = "~" + markdownString[entity.Offset]
			markdownString[entity.Offset+entity.Length-1] += "~"
		case entity.Type == tele.EntitySpoiler:
			markdownString[entity.Offset] = "||" + markdownString[entity.Offset]
			markdownString[entity.Offset+entity.Length-1] += "||"
		case entity.Type == tele.EntityCode:
			markdownString[entity.Offset] = "`" + markdownString[entity.Offset]
			markdownString[entity.Offset+entity.Length-1] += "`"
		case entity.Type == tele.EntityCodeBlock:
			markdownString[entity.Offset] = "```" + markdownString[entity.Offset]
			markdownString[entity.Offset+entity.Length-1] += "```"
		case entity.Type == tele.EntityTextLink:
			markdownString[entity.Offset] = "[" + markdownString[entity.Offset]
			markdownString[entity.Offset+entity.Length-1] += fmt.Sprintf("](%s)", escapeTgMarkdownV2SpecialSymbols(entity.URL))
		default:
			log.Printf("entity of type %s is not supported", entity.Type)
		}
	}

	return strings.Join(markdownString, "")
}

func contains(array []string, value string) bool {
	for _, el := range array {
		if el == value {
			return true
		}
	}
	return false
}
