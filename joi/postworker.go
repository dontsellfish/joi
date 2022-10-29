package joi

import (
	"errors"
	"fmt"
	tele "gopkg.in/telebot.v3"
	"log"
	"os"
	"path"
	"sync"
	"time"
)

type PostWorker struct {
	Joi               *Joi
	PollingTimeout    time.Duration
	TimeoutForSources time.Duration
	OnError           func(error)

	lastPosts    map[int]string // post id in the channel -> post_id in database
	postingMutex sync.Mutex
}

func NewPostWorker(joi *Joi, period ...time.Duration) *PostWorker {
	period_ := time.Minute
	if len(period) > 0 {
		period_ = period[0]
	}

	return &PostWorker{
		Joi:               joi,
		PollingTimeout:    period_,
		TimeoutForSources: time.Minute,
		OnError:           func(error) {},
		lastPosts:         map[int]string{},
		postingMutex:      sync.Mutex{},
	}
}

func (worker *PostWorker) isDefaultPostTime(t string) bool {
	for _, postTime := range worker.Joi.Cfg.DefaultPostTimes {
		if t == postTime {
			return true
		}
	}
	return false
}

func (worker *PostWorker) Start() {
	time.Sleep(time.Duration(60+5-time.Now().Second()) * time.Second)
	_, err := worker.PostForTime(time.Now().Format("15:04"))
	if err != nil {
		worker.OnError(err)
	}

	for tick := range time.Tick(worker.PollingTimeout) {
		_, err = worker.PostForTime(tick.Format("15:04"))
		if err != nil {
			worker.OnError(err)
		}
	}
}

func (worker *PostWorker) PostForTime(time string) ([]tele.Message, error) {
	post, err := worker.Joi.Database.GetRandomPostByTime(time)
	if IsErrRedisNotFound(err) && worker.isDefaultPostTime(time) {
		post, err = worker.Joi.Database.GetRandomPostByTime(TimeIsNotSpecified)
		if err != nil {
			return nil, err
		} else {
			return worker.Post(post)
		}
	} else if err != nil {
		return nil, err
	} else {
		return worker.Post(post)
	}
}

func (worker *PostWorker) Post(post *PostInfo) ([]tele.Message, error) {
	return worker.PostExtended(post, worker.Joi.Cfg.ChannelId, worker.genSendOptions(post.IsProtected), true)
}

func (worker *PostWorker) genSendOptions(isProtected bool) *tele.SendOptions {
	return &tele.SendOptions{
		ReplyTo:               nil,
		ReplyMarkup:           nil,
		DisableWebPagePreview: worker.Joi.Cfg.DisableWebPagePreview,
		DisableNotification:   worker.Joi.Cfg.DisableNotification,
		ParseMode:             worker.Joi.Cfg.ParseMode,
		AllowWithoutReply:     true,
		Protected:             isProtected,
	}
}

func (worker *PostWorker) PostExtended(post *PostInfo, chatId int64, opts *tele.SendOptions, deleteFromDatabase bool) ([]tele.Message, error) {
	album, sources, downloaded, err := worker.Joi.postInfoToTelegramAlbum(post)
	defer func() {
		for _, file := range downloaded {
			err := os.Remove(file)
			if err != nil {
				worker.OnError(err)
			}
		}
	}()
	if err != nil {
		return nil, err
	}

	if opts == nil {
		opts = worker.genSendOptions(post.IsProtected)
	}
	messages, err := worker.Joi.Bot.SendAlbum(&tele.Chat{ID: chatId}, album, opts)
	if err != nil {
		return nil, err
	}
	worker.AddPosted(post, messages...)

	if post.PostSources == PostSourcesAuto {
		post.PostSources = PostSourcesFalse
	}
	switch post.PostSources {
	case PostSourcesTrue:
		if chatId == worker.Joi.Cfg.ChannelId {
			go worker.sourcePostingPolling(post.Id, sources, deleteFromDatabase)()
		} else {
			_, err := worker.Joi.Bot.SendAlbum(&tele.Chat{ID: chatId}, sources,
				&tele.SendOptions{
					Protected: post.IsProtected,
					ParseMode: worker.Joi.Cfg.ParseMode,
				})
			if err != nil {
				return nil, err
			}
		}
	case PostSourcesFalse:
		if post.Comment != "" {
			if chatId == worker.Joi.Cfg.ChannelId {
				go worker.sourcePostingPolling(post.Id, post.Comment, deleteFromDatabase)()
			} else {
				_, err := worker.Joi.Bot.Send(&tele.Chat{ID: chatId}, post.Comment,
					&tele.SendOptions{
						Protected: post.IsProtected,
						ParseMode: worker.Joi.Cfg.ParseMode,
					})
				if err != nil {
					return nil, err
				}
			}
		} else {
			if deleteFromDatabase {
				err = worker.Joi.Database.RemovePost(post.Id)
				if err != nil {
					worker.OnError(errors.New(fmt.Sprintf("while posting sources for `%s`\nan error occured:%s", post.Id, err.Error())))
				}
			}
		}
	case PostSourcesAuto:
		log.Printf("not supported right now :c")
	}

	return messages, nil
}

func (worker *PostWorker) sourcePostingPolling(postId string, comment interface{}, deleteFromDatabase bool) func() {
	return func() {
		if r := recover(); r != nil {
			worker.OnError(errors.New(fmt.Sprintf("%v", r)))
		}
		endOfPolling := time.Now().Add(worker.TimeoutForSources)
		for t := range time.Tick(time.Second * 10) {
			if t.After(endOfPolling) {
				worker.OnError(errors.New(fmt.Sprintf("sources for PostExtended `%s`, never have been actually posted", postId)))
				break
			}

			post, err := worker.Joi.Database.GetPost(postId)
			if err != nil {
				worker.OnError(errors.New(fmt.Sprintf("retrieving PostExtended info `%s`\nan error occured:%s", postId, err.Error())))
				continue
			}
			if post.MsgIdInCommentsChat != 0 {
				switch comment.(type) {
				case tele.Album:
					_, err = worker.Joi.Bot.SendAlbum(&tele.Chat{ID: worker.Joi.Cfg.CommentsId},
						comment.(tele.Album),
						&tele.SendOptions{
							ReplyTo:   &tele.Message{ID: post.MsgIdInCommentsChat, Chat: &tele.Chat{ID: worker.Joi.Cfg.CommentsId}},
							Protected: post.IsProtected,
							ParseMode: worker.Joi.Cfg.ParseMode,
						},
					)
				case string:
					_, err = worker.Joi.Bot.Send(&tele.Chat{ID: worker.Joi.Cfg.CommentsId},
						comment.(string),
						&tele.SendOptions{
							ReplyTo:   &tele.Message{ID: post.MsgIdInCommentsChat, Chat: &tele.Chat{ID: worker.Joi.Cfg.CommentsId}},
							Protected: post.IsProtected,
							ParseMode: worker.Joi.Cfg.ParseMode,
						},
					)
				default:
					err = errors.New("unsupported type of comment is provided")
				}
				if err != nil {
					worker.OnError(errors.New(fmt.Sprintf("while posting sources for `%s`\nan error occured:%s", postId, err.Error())))
				}
				if deleteFromDatabase {
					err = worker.Joi.Database.RemovePost(post.Id)
					if err != nil {
						worker.OnError(errors.New(fmt.Sprintf("while posting sources for `%s`\nan error occured:%s", postId, err.Error())))
					}
				}

				break
			}
		}
	}
}

func (joi *Joi) postInfoToTelegramAlbum(post *PostInfo) (album tele.Album, sources tele.Album, downloaded []string, err error) {
	album = make(tele.Album, 0)
	sources = make(tele.Album, 0)
	downloaded = make([]string, 0)
	for i, file := range post.Files {
		caption := ""
		if i+1 == len(post.Files) {
			caption = post.Text
		}
		comment := ""
		if i+1 == len(post.Files) {
			comment = post.Comment
		}

		fileOnServer, err := joi.Bot.FileByID(file.Id)
		if err != nil {
			return nil, nil, downloaded, err
		}

		switch file.Type {
		case TelegramFileTypePhoto:
			album = append(album, &tele.Photo{
				File:    fileOnServer,
				Caption: caption,
			})
		case TelegramFileTypeVideo:
			album = append(album, &tele.Video{
				File:    fileOnServer,
				Caption: caption,
			})
		case TelegramFileTypeDocPhoto:
			localFileName := path.Join(joi.Cfg.TemporaryFilesDirectory, file.Id)
			err = joi.Bot.Download(&fileOnServer, localFileName)
			if err != nil {
				return nil, nil, downloaded, err
			}
			downloaded = append(downloaded, localFileName)

			info, err := joi.Converter.IdentifyImage(localFileName)
			if err != nil {
				return nil, nil, downloaded, err
			}
			if IsShouldBeConvertedToBePostedOnTelegram(info) {
				localFileName, err = joi.Converter.Image(localFileName)
				if err != nil {
					return nil, nil, downloaded, err
				}
				downloaded = append(downloaded, localFileName)
			}

			album = append(album, &tele.Photo{
				File:    tele.FromDisk(localFileName),
				Caption: caption,
			})

			sources = append(sources, &tele.Document{
				File:    fileOnServer,
				Caption: comment,
			})
		case TelegramFileTypeDocVideo:
			localFileName := path.Join(joi.Cfg.TemporaryFilesDirectory, file.Id)
			err = joi.Bot.Download(&fileOnServer, localFileName)
			if err != nil {
				return nil, nil, downloaded, err
			}
			downloaded = append(downloaded, localFileName)
			album = append(album, &tele.Video{
				File:    tele.FromDisk(localFileName),
				Caption: caption,
			})

			sources = append(sources, &tele.Document{
				File:    fileOnServer,
				Caption: comment,
			})
		}
	}
	return album, sources, downloaded, nil
}

func (joi *Joi) postInfoToTelegramDocumentsAlbum(post *PostInfo) (album tele.Album, err error) {
	album = make(tele.Album, 0)
	for i, file := range post.Files {
		caption := ""
		if i+1 == len(post.Files) {
			caption = post.Comment
		}
		fileOnServer, err := joi.Bot.FileByID(file.Id)
		if err != nil {
			return nil, err
		}

		switch file.Type {
		case TelegramFileTypePhoto:
			album = append(album, &tele.Document{
				File:    fileOnServer,
				Caption: caption,
			})
		case TelegramFileTypeVideo:
			album = append(album, &tele.Document{
				File:    fileOnServer,
				Caption: caption,
			})
		}
	}
	return album, nil
}

func (worker *PostWorker) AddPosted(post *PostInfo, messages ...tele.Message) {
	defer worker.postingMutex.Unlock()
	worker.postingMutex.Lock()
	for _, msg := range messages {
		worker.lastPosts[msg.ID] = post.Id
	}

	go func() {
		defer worker.postingMutex.Unlock()
		time.Sleep(worker.TimeoutForSources * 2)
		worker.postingMutex.Lock()
		for _, msg := range messages {
			delete(worker.lastPosts, msg.ID)
		}
	}()
}

func (worker *PostWorker) GetPosted(messageId int) (string, bool) {
	defer worker.postingMutex.Unlock()
	worker.postingMutex.Lock()

	id, ok := worker.lastPosts[messageId]
	return id, ok
}
