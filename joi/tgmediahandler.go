package joi

import (
	"encoding/json"
	"errors"
	"fmt"
	tele "gopkg.in/telebot.v3"
	"time"
)

const DefaultTimeout = time.Second * 1

type MediaGroupsHandler struct {
	Timeout time.Duration
	Handler func(messages []*tele.Message) error
	Bot     *tele.Bot

	groups map[string][]*tele.Message
}

func NewMediaGroupsHandler(bot *tele.Bot, handler func([]*tele.Message) error) *MediaGroupsHandler {
	return &MediaGroupsHandler{
		Timeout: DefaultTimeout,
		Handler: handler,
		Bot:     bot,
		groups:  make(map[string][]*tele.Message),
	}
}

func (handler *MediaGroupsHandler) Register() func(tele.Context) error {
	return func(ctx tele.Context) error {
		message := deepCopyViaJsonSorryJesusChrist(ctx.Message())
		switch {
		case message.Voice != nil:
			return errors.New("voice messages are not supported right now")
		case message.Sticker != nil:
			return errors.New("stickers are not supported right now")
		case message.VideoNote != nil:
			return errors.New("video notes are not supported right now")
		}

		id := mediaGroupToId(message)
		if _, contains := handler.groups[id]; !contains {
			handler.groups[id] = []*tele.Message{message}

			go func() {
				defer func() {
					delete(handler.groups, id)
					if r := recover(); r != nil {
						handler.Bot.OnError(errors.New(fmt.Sprintf("%v", r)),
							handler.Bot.NewContext(tele.Update{Message: deepCopyViaJsonSorryJesusChrist(message)}))
					}
				}()
				if message.AlbumID != "" {
					time.Sleep(handler.Timeout)
				}
				err := handler.Handler(handler.groups[mediaGroupToId(message)])
				if err != nil {
					handler.Bot.OnError(err, handler.Bot.NewContext(tele.Update{Message: deepCopyViaJsonSorryJesusChrist(message)}))
				}
			}()
		} else {
			handler.groups[id] = append(handler.groups[id], message)
		}

		return nil
	}
}

func deepCopyViaJsonSorryJesusChrist[T any](obj *T) *T {
	buff, err := json.Marshal(obj)
	if err != nil {
		panic(err) // unexpected behavior
	}
	var copied T
	err = json.Unmarshal(buff, &copied)
	if err != nil {
		panic(err) // much more unexpected behavior
	}
	return &copied
}
