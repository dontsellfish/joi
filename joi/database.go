package joi

import (
	"context"
	"errors"
	"fmt"
	"github.com/go-redis/redis/v8"
	tele "gopkg.in/telebot.v3"
	"log"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

/* This comment might be outdated, tho it should help you to understand the database structure
Redis as DB:
	Structure:
		{
			joi:bot_id:times				: set<post_time>
			joi:bot_id:time:time_value		: set<post_id>
			joi:bot_id:posts				: set<post_id>

			joi:bot_id:post:id:time			: time
			joi:bot_id:post:id:text			: post_text.md
			joi:bot_id:post:id:comment		: comment_text.md
			joi:bot_id:post:id:post_src		: post_sources
			joi:bot_id:post:id:protected	: is_protected
			joi:bot_id:post:id:files		: file_type_1 tg_file_id_1 file_type_2 tg_file_id_2...
			joi:bot_id:post:id:release_id	: msg_id_in_comments_chat_channel_posted
			joi:bot_id:post:id:msg_ids		: admin_id msg_id_1 msg_id_2...
			...

			joi:bot_id:admin_id:msg_id		: post_id
			...
		}

	Note:
		- file_type in {photo, video, doc_photo, doc_video}
		- post_sources in {true, false, auto}
		- is_protected in {true, false}
		- post_text.md, comment_text.md - markdown strings, not actual files

	class Database:
		func GetTimes() -> List[TimeString] or Error

		func GetPosts() -> List[PostInfo] or Error
		func GetPost(msg_id or post_id) -> PostInfo or Error
		func GetPostsByTime(TimeString) -> List[PostInfo] or Error

		func AddPostFromMessages(telegram.Message...) -> PostInfo or Error
		func ChangePost(msg_id or post_id, PostInfo) -> PostInfo or Error
		func RemovePost(msg_id or post_id) -> new PostInfo or Error
*/

var redisContext = context.Background()

type Database struct {
	mutex  sync.Mutex
	client *redis.Client
	prefix string
}

func NewDatabase(prefix string, opt *redis.Options) *Database {
	return &Database{
		mutex:  sync.Mutex{},
		client: redis.NewClient(opt),
		prefix: prefix,
	}
}

func (db *Database) toKey(args ...string) string {
	entities := []string{db.prefix}
	entities = append(entities, args...)
	return strings.Join(entities, ":")
}

func (db *Database) GetTimes() (times []string, err error) {
	defer db.mutex.Unlock()
	db.mutex.Lock()
	times, err = db.client.SMembers(redisContext, db.toKey("times")).Result()
	if err != nil {
		return nil, err
	}
	sort.Strings(times)
	return times, nil
}

func (db *Database) GetPost(id string) (post *PostInfo, err error) {
	defer db.mutex.Unlock()
	db.mutex.Lock()
	return db.getPostAsync(id)
}

func (db *Database) GetPosts() (posts []*PostInfo, err error) {
	defer db.mutex.Unlock()
	db.mutex.Lock()

	ids, err := db.client.SMembers(redisContext, db.toKey("posts")).Result()
	if err != nil {
		return nil, err
	}

	posts = make([]*PostInfo, len(ids))
	for i, id := range ids {
		posts[i], err = db.getPostAsync(id)
		if err != nil {
			return nil, err
		}
	}

	return
}

func (db *Database) GetPostsByTime(t string) (posts []*PostInfo, err error) {
	defer db.mutex.Unlock()
	db.mutex.Lock()

	if !isTimeValid(t) {
		return nil, errors.New(fmt.Sprintf("%s is invalid TimeString", t))
	}

	ids, err := db.client.SMembers(redisContext, db.toKey("time", t)).Result()
	if err != nil {
		return nil, err
	}

	posts = make([]*PostInfo, len(ids))
	for i, id := range ids {
		posts[i], err = db.getPostAsync(id)
		if err != nil {
			return nil, err
		}
	}

	return
}

func (db *Database) GetRandomPostByTime(t string) (post *PostInfo, err error) {
	defer db.mutex.Unlock()
	db.mutex.Lock()

	if !isTimeValid(t) {
		return nil, errors.New(fmt.Sprintf("%s is invalid TimeString", t))
	}

	id, err := db.client.SRandMember(redisContext, db.toKey("time", t)).Result()
	if err != nil {
		return nil, err
	}

	return db.getPostAsync(id)
}

func (db *Database) AddPost(post *PostInfo) (*PostInfo, error) {
	defer db.mutex.Unlock()
	db.mutex.Lock()

	return db.putPostAsync(post)
}
func (db *Database) AddPostFromMessages(base *PostInfo, msgs ...*tele.Message) (post *PostInfo, err error) {
	defer db.mutex.Unlock()
	db.mutex.Lock()

	if len(msgs) == 0 {
		return nil, errors.New("no messages provided")
	}
	if msgs[0].Sender == nil {
		return nil, errors.New("broken msg is given")
	}

	id := mediaGroupToId(msgs[0])
	post = &PostInfo{
		Id:                  id,
		Time:                base.Time,
		Text:                base.Text,
		Comment:             base.Comment,
		PostSources:         base.PostSources,
		IsProtected:         base.IsProtected,
		Files:               make([]TgFileInfo, len(msgs)),
		MsgIdInCommentsChat: base.MsgIdInCommentsChat,
		AdminPostedId:       msgs[0].Sender.ID,
		OriginalMsgIds:      make([]int64, len(msgs)),
	}

	for i, msg := range msgs {
		post.OriginalMsgIds[i] = int64(msg.ID)

		switch {
		case msg.Photo != nil:
			post.Files[i] = TgFileInfo{TelegramFileTypePhoto, msg.Photo.FileID}
		case msg.Video != nil:
			post.Files[i] = TgFileInfo{TelegramFileTypeVideo, msg.Video.FileID}
		case msg.Document != nil && strings.HasPrefix(strings.ToLower(msg.Document.MIME), "image"):
			post.Files[i] = TgFileInfo{TelegramFileTypeDocPhoto, msg.Document.FileID}
		case msg.Document != nil && strings.HasPrefix(strings.ToLower(msg.Document.MIME), "video"):
			if msg.Document.FileSize < 50_000_000 {
				post.Files[i] = TgFileInfo{TelegramFileTypeDocVideo, msg.Document.FileID}
			} else {
				return nil, errors.New(fmt.Sprintf("File of size %.2f MB is too big (max - 50MB)",
					float64(msg.Document.FileSize)/1_000_000.0))
			}
		default:
			return nil, errors.New("message with no supported media is provided")
		}
	}

	return db.putPostAsync(post)
}

func (db *Database) ChangePost(id string, what int, value interface{}) (*PostInfo, error) {
	defer db.mutex.Unlock()
	db.mutex.Lock()

	post, err := db.getPostAsync(id)
	if err != nil {
		return nil, err
	}

	switch what {
	case ChangePostTime:
		newTime := value.(string)
		if !isTimeValid(newTime) {
			return nil, errors.New(fmt.Sprintf("time %s is invalid", newTime))
		}
		post.Time = newTime
	case ChangePostText:
		post.Text = value.(string)
	case ChangePostComment:
		post.Comment = value.(string)
	case ChangePostIsProtected:
		post.IsProtected = value.(bool)
	case ChangePostPostSources:
		post.PostSources = value.(int)
	case ChangePostMsgIdInCommentsChat:
		post.MsgIdInCommentsChat = value.(int)
	}

	err = db.remPostAsync(id)
	if err != nil {
		log.Printf("warning: while removing %s, errors occured:\n%s", id, err.Error())
	}

	return db.putPostAsync(post)
}

func (db *Database) containsPostAsync(id string) (bool, error) {
	return db.client.SIsMember(redisContext, db.toKey("posts"), id).Result()
}

func (db *Database) ContainsPost(id string) (bool, error) {
	defer db.mutex.Unlock()
	db.mutex.Lock()
	return db.client.SIsMember(redisContext, db.toKey("posts"), id).Result()
}

func (db *Database) RemovePost(id string) (err error) {
	defer db.mutex.Unlock()
	db.mutex.Lock()
	return db.remPostAsync(id)
}

func mediaGroupToId(msg *tele.Message) string {
	if msg.AlbumID != "" {
		return msg.AlbumID
	} else {
		return fmt.Sprintf("%d_%d", msg.Chat.ID, msg.ID)
	}
}

func isTimeValid(t string) bool {
	reg := regexp.MustCompile("^([0-1][0-9]|2[0-3]):[0-5][0-9]$")
	return reg.MatchString(strings.Trim(t, " ")) || t == "" || strings.ToUpper(t) == TimeIsNotSpecified
}

func isPostInfoValid(info *PostInfo) bool {
	return info != nil && info.Id != "" && isTimeValid(info.Time) && len(info.Text) < 4096 && len(info.Comment) < 4096 && info.AdminPostedId != 0 &&
		info.PostSources >= 0 && info.PostSources <= 3 && len(info.Files) > 0 && info.MsgIdInCommentsChat >= 0 && len(info.OriginalMsgIds) > 0
}

func (db *Database) getPostAsync(id string) (post *PostInfo, err error) {
	post = &PostInfo{
		Id:                  id,
		Time:                "",
		Text:                "",
		Comment:             "",
		PostSources:         0,
		IsProtected:         false,
		Files:               nil,
		MsgIdInCommentsChat: 0,
		OriginalMsgIds:      nil,
	}

	post.Time, err = db.client.Get(redisContext, db.toKey("post", id, "time")).Result()
	if err != nil {
		return nil, err
	}
	post.Text, err = db.client.Get(redisContext, db.toKey("post", id, "text")).Result()
	if err != nil {
		return nil, err
	}
	post.Comment, err = db.client.Get(redisContext, db.toKey("post", id, "comment")).Result()
	if err != nil {
		return nil, err
	}
	post.PostSources, err = db.client.Get(redisContext, db.toKey("post", id, "post_sources")).Int()
	if err != nil {
		return nil, err
	}
	post.IsProtected, err = db.client.Get(redisContext, db.toKey("post", id, "is_protected")).Bool()
	if err != nil {
		return nil, err
	}
	fileInfos, err := db.client.LRange(redisContext, db.toKey("post", id, "files"), 0, -1).Result()
	if err != nil {
		return nil, err
	}
	post.Files = make([]TgFileInfo, len(fileInfos))
	for i, info := range fileInfos {
		if len(info) < 3 {
			return nil, errors.New(fmt.Sprintf("'%s' file info is invalid formatted", info))
		}

		post.Files[i].Type, err = strconv.Atoi(info[:1])
		if err != nil {
			return nil, errors.New(fmt.Sprintf("%s\nfor file info '%s'", err.Error(), info))
		}
		post.Files[i].Id = info[2:]
	}
	post.MsgIdInCommentsChat, err = db.client.Get(redisContext, db.toKey("post", id, "release_id")).Int()
	if err != nil {
		return nil, err
	}
	adminAndMsgIds, err := db.client.LRange(redisContext, db.toKey("post", id, "msg_ids"), 0, -1).Result()
	if err != nil {
		return nil, err
	}
	post.AdminPostedId, err = strconv.ParseInt(adminAndMsgIds[0], 10, 64)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("%s\nfor file info '%s'", err.Error(), adminAndMsgIds[0]))
	}
	post.OriginalMsgIds = make([]int64, len(adminAndMsgIds)-1)
	for i, info := range adminAndMsgIds[1:] {
		post.OriginalMsgIds[i], err = strconv.ParseInt(info, 10, 64)
		if err != nil {
			return nil, errors.New(fmt.Sprintf("%s\nfor file info '%s'", err.Error(), info))
		}
	}

	return
}

func (db *Database) putPostAsync(new *PostInfo) (post *PostInfo, err error) {
	if !isPostInfoValid(new) {
		return nil, errors.New("new post is not valid")
	}

	id := new.Id

	contains, err := db.containsPostAsync(id)
	if err != nil {
		return nil, err
	}
	if contains {
		return nil, errors.New(fmt.Sprintf("post with id %s already exists", id))
	}

	var newTime string
	if string(new.Time) == "" {
		newTime = TimeIsNotSpecified
	} else {
		newTime = new.Time
	}

	// structure fields //

	err = db.client.Set(redisContext, db.toKey("post", id, "time"), newTime, 0).Err()
	if err != nil {
		return nil, err
	}
	err = db.client.Set(redisContext, db.toKey("post", id, "text"), new.Text, 0).Err()
	if err != nil {
		return nil, err
	}
	err = db.client.Set(redisContext, db.toKey("post", id, "comment"), new.Comment, 0).Err()
	if err != nil {
		return nil, err
	}
	err = db.client.Set(redisContext, db.toKey("post", id, "post_sources"), new.PostSources, 0).Err()
	if err != nil {
		return nil, err
	}
	err = db.client.Set(redisContext, db.toKey("post", id, "is_protected"), fmt.Sprintf("%t", new.IsProtected), 0).Err()
	if err != nil {
		return nil, err
	}
	for _, fileInfo := range new.Files {
		err = db.client.RPush(redisContext, db.toKey("post", id, "files"), fmt.Sprintf("%d %s", fileInfo.Type, fileInfo.Id)).Err()
		if err != nil {
			return nil, err
		}
	}
	err = db.client.Set(redisContext, db.toKey("post", id, "release_id"), new.MsgIdInCommentsChat, 0).Err()
	if err != nil {
		return nil, err
	}
	err = db.client.RPush(redisContext, db.toKey("post", id, "msg_ids"), fmt.Sprintf("%d", new.AdminPostedId)).Err()
	if err != nil {
		return nil, err
	}
	for _, msgId := range new.OriginalMsgIds {
		err = db.client.RPush(redisContext, db.toKey("post", id, "msg_ids"), fmt.Sprintf("%d", msgId)).Err()
		if err != nil {
			return nil, err
		}
	}

	// side effects //

	err = db.client.SAdd(redisContext, db.toKey("time", newTime), new.Id).Err()
	if err != nil {
		return nil, err
	}

	err = db.client.SAdd(redisContext, db.toKey("posts"), new.Id).Err()
	if err != nil {
		return nil, err
	}

	err = db.client.SAdd(redisContext, db.toKey("times"), newTime).Err()
	if err != nil {
		return nil, err
	}

	for _, msgId := range new.OriginalMsgIds {
		err = db.client.Set(redisContext, db.toKey(fmt.Sprintf("%d", new.AdminPostedId), fmt.Sprintf("%d", msgId)), new.Id, 0).Err()
		if err != nil {
			return nil, err
		}
	}

	return db.getPostAsync(id)
}

func (db *Database) remPostAsync(id string) error {
	delPost, _ := db.getPostAsync(id)

	var occurredErrors []string
	var err error

	// structure fields //

	contains, err := db.containsPostAsync(id)
	if err != nil {
		occurredErrors = append(occurredErrors, err.Error())
	}

	if !contains {
		return nil
	}

	err = db.client.Del(redisContext, db.toKey("post", id, "text")).Err()
	if err != nil {
		occurredErrors = append(occurredErrors, err.Error())
	}
	err = db.client.Del(redisContext, db.toKey("post", id, "time")).Err()
	if err != nil {
		occurredErrors = append(occurredErrors, err.Error())
	}
	err = db.client.Del(redisContext, db.toKey("post", id, "comment")).Err()
	if err != nil {
		occurredErrors = append(occurredErrors, err.Error())
	}
	err = db.client.Del(redisContext, db.toKey("post", id, "post_sources")).Err()
	if err != nil {
		occurredErrors = append(occurredErrors, err.Error())
	}
	err = db.client.Del(redisContext, db.toKey("post", id, "is_protected")).Err()
	if err != nil {
		occurredErrors = append(occurredErrors, err.Error())
	}
	err = db.client.Del(redisContext, db.toKey("post", id, "files")).Err()
	if err != nil {
		occurredErrors = append(occurredErrors, err.Error())
	}
	err = db.client.Del(redisContext, db.toKey("post", id, "release_id")).Err()
	if err != nil {
		occurredErrors = append(occurredErrors, err.Error())
	}
	err = db.client.Del(redisContext, db.toKey("post", id, "msg_ids")).Err()
	if err != nil {
		occurredErrors = append(occurredErrors, err.Error())
	}

	// side effects //

	err = db.client.SRem(redisContext, db.toKey("posts"), id).Err()
	if err != nil {
		occurredErrors = append(occurredErrors, err.Error())
	}

	if delPost != nil {
		if isTimeValid(delPost.Time) {
			timesNumber, err := db.client.SCard(redisContext, db.toKey("time", delPost.Time)).Result()
			if err == nil {
				if timesNumber == 1 {
					err = db.client.SRem(redisContext, db.toKey("times"), delPost.Time).Err()
					if err != nil {
						occurredErrors = append(occurredErrors, err.Error())
					}
				}
			} else {
				occurredErrors = append(occurredErrors, err.Error())
			}
			err = db.client.SRem(redisContext, db.toKey("time", delPost.Time), delPost.Id).Err()
			if err != nil {
				occurredErrors = append(occurredErrors, err.Error())
			}
		}
		if len(delPost.OriginalMsgIds) > 0 && delPost.AdminPostedId != 0 {
			for _, msgId := range delPost.OriginalMsgIds {
				err = db.client.Del(redisContext, db.toKey(fmt.Sprintf("%d", delPost.AdminPostedId), fmt.Sprintf("%d", msgId))).Err()
				if err != nil {
					occurredErrors = append(occurredErrors, err.Error())
				}
			}
		}
	}

	if len(occurredErrors) > 0 {
		return errors.New(strings.Join(occurredErrors, "\n"))
	} else {
		return nil
	}
}

func IsErrRedisNotFound(err error) bool {
	return err != nil && err.Error() == "redis: nil"
}
