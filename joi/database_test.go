package joi

import (
	"fmt"
	"github.com/go-redis/redis/v8"
	"strings"
	"testing"
)

const redisTestingDatabaseKeyPrefix = "joi:testing"

var redisTestingConfig = &redis.Options{
	Addr: "localhost:6379",
	DB:   1,
}
var db *Database

var (
	testPost1111 = PostInfo{
		Id:          "testPost1111",
		Time:        "11:11",
		Text:        "test text 1111",
		Comment:     "test comment 1111",
		PostSources: PostSourcesTrue,
		IsProtected: true,
		Files: []TgFileInfo{{TelegramFileTypePhoto, "kitty photo 1111"},
			{TelegramFileTypeVideo, "corgi video 1111"}},
		MsgIdInCommentsChat: 777,
		AdminPostedId:       1000,
		OriginalMsgIds:      []int64{7, 8},
	}
	testPostNA = PostInfo{
		Id:          "testPostNA",
		Time:        TimeIsNotSpecified,
		Text:        "test text 2222",
		Comment:     "test comment 2222",
		PostSources: PostSourcesTrue,
		IsProtected: true,
		Files: []TgFileInfo{{TelegramFileTypeDocPhoto, "kitty photo 2222"},
			{TelegramFileTypeDocVideo, "corgi video 2222"}},
		MsgIdInCommentsChat: 0,
		AdminPostedId:       1000,
		OriginalMsgIds:      []int64{12, 13},
	}
)

func unwrap(equal bool, reason string) bool {
	return equal
}

func arePostsEqual(lhs *PostInfo, rhs *PostInfo) (equal bool, reason string) {
	if lhs.Id != rhs.Id {
		return false, fmt.Sprintf("PostExtended.Id != testPost_11_11.Id, %s != %s", lhs.Id, rhs.Id)
	}
	if lhs.Time != rhs.Time {
		return false, fmt.Sprintf("lhs.Time != rhs.Time, %s != %s", lhs.Time, rhs.Time)
	}
	if lhs.Text != rhs.Text {
		return false, fmt.Sprintf("lhs.Text != rhs.Text, %s != %s", lhs.Text, rhs.Text)
	}
	if lhs.Comment != rhs.Comment {
		return false, fmt.Sprintf("lhs.Comment != rhs.Comment, %s != %s", lhs.Comment, rhs.Comment)
	}
	if lhs.PostSources != rhs.PostSources {
		return false, fmt.Sprintf("lhs.PostSources != rhs.PostSources, %d != %d", lhs.PostSources, rhs.PostSources)
	}
	if lhs.IsProtected != rhs.IsProtected {
		return false, fmt.Sprintf("lhs.IsProtected != rhs.IsProtected, %t != %t", lhs.IsProtected, rhs.IsProtected)
	}
	if lhs.MsgIdInCommentsChat != rhs.MsgIdInCommentsChat {
		return false, fmt.Sprintf("lhs.MsgIdInCommentsChat != rhs.MsgIdInCommentsChat, %d != %d", lhs.MsgIdInCommentsChat, rhs.MsgIdInCommentsChat)
	}
	if lhs.AdminPostedId != rhs.AdminPostedId {
		return false, fmt.Sprintf("lhs.AdminPostedId != rhs.AdminPostedId, %d != %d", lhs.AdminPostedId, rhs.AdminPostedId)
	}
	if lhs.Files[0].Id != rhs.Files[0].Id {
		return false, fmt.Sprintf("lhs.Files[0].Id != rhs.Files[0].Id, %s != %s", lhs.Files[0].Id, rhs.Files[0].Id)
	}
	if lhs.Files[1].Id != rhs.Files[1].Id {
		return false, fmt.Sprintf("lhs.Files[1].Id != rhs.Files[1].Id, %s != %s", lhs.Files[1].Id, rhs.Files[1].Id)
	}
	if lhs.Files[0].Type != rhs.Files[0].Type {
		return false, fmt.Sprintf("lhs.Files[0].Type != rhs.Files[0].Type, %d != %d", lhs.Files[0].Type, rhs.Files[0].Type)
	}
	if lhs.Files[1].Type != rhs.Files[1].Type {
		return false, fmt.Sprintf("lhs.Files[1].Type != rhs.Files[1].Type, %d != %d", lhs.Files[1].Type, rhs.Files[1].Type)
	}
	if lhs.OriginalMsgIds[0] != rhs.OriginalMsgIds[0] {
		return false, fmt.Sprintf("lhs.OriginalMsgIds[0] != rhs.OriginalMsgIds[0], %d != %d", lhs.OriginalMsgIds[0], rhs.OriginalMsgIds[0])
	}
	return true, ""
}

func TestNewDatabase(t *testing.T) {
	db = NewDatabase(redisTestingDatabaseKeyPrefix, redisTestingConfig)
	if db == nil {
		t.Fatal("db == nil")
	}
	err := db.client.FlushDB(redisContext).Err()
	if err != nil {
		t.Fatal(err.Error())
	}
}

func TestDatabase_AddPost(t *testing.T) {
	db = NewDatabase(redisTestingDatabaseKeyPrefix, redisTestingConfig)
	err := db.client.FlushDB(redisContext).Err()
	if err != nil {
		t.Fatal(err.Error())
	}

	post, err := db.AddPost(&testPost1111)
	if err != nil {
		t.Fatal(err.Error())
	}

	equal, reason := arePostsEqual(&testPost1111, post)
	if !equal {
		t.Fatalf("lhs - test PostExtended, rhs - what is really posted\n%s", reason)
	}
}

func TestDatabase_RemovePost(t *testing.T) {
	db = NewDatabase(redisTestingDatabaseKeyPrefix, redisTestingConfig)
	err := db.client.FlushDB(redisContext).Err()
	if err != nil {
		t.Fatal(err.Error())
	}

	post, err := db.AddPost(&testPost1111)
	if err != nil {
		t.Fatal(err.Error())
	}

	err = db.RemovePost(post.Id)
	if err != nil {
		t.Fatal(err.Error())
	}

	keys, err := db.client.Keys(redisContext, "*").Result()
	if err != nil {
		t.Fatal(err.Error())
	}

	if len(keys) > 0 {
		t.Fatalf("not all keys where removed, left:\n%s", strings.Join(keys, "\n"))
	}
}

func TestDatabase_GetTimes(t *testing.T) {
	db = NewDatabase(redisTestingDatabaseKeyPrefix, redisTestingConfig)
	err := db.client.FlushDB(redisContext).Err()
	if err != nil {
		t.Fatal(err.Error())
	}

	_, err = db.AddPost(&testPost1111)
	if err != nil {
		t.Fatal(err.Error())
	}
	_, err = db.AddPost(&testPostNA)
	if err != nil {
		t.Fatal(err.Error())
	}

	times, err := db.GetTimes()
	if err != nil {
		t.Fatal(err.Error())
	}

	if times[0] != "11:11" || times[1] != TimeIsNotSpecified {
		t.Fatalf("times[0] != \"11:11\" || times[1] != \"NA\", times=[%s, %s]", times[0], times[1])
	}
}

func TestDatabase_GetPosts(t *testing.T) {
	db = NewDatabase(redisTestingDatabaseKeyPrefix, redisTestingConfig)
	err := db.client.FlushDB(redisContext).Err()
	if err != nil {
		t.Fatal(err.Error())
	}

	_, err = db.AddPost(&testPost1111)
	if err != nil {
		t.Fatal(err.Error())
	}
	_, err = db.AddPost(&testPostNA)
	if err != nil {
		t.Fatal(err.Error())
	}

	posts, err := db.GetPosts()
	if err != nil {
		t.Fatal(err.Error())
	}

	if !(unwrap(arePostsEqual(posts[0], &testPost1111)) && unwrap(arePostsEqual(posts[1], &testPostNA)) ||
		unwrap(arePostsEqual(posts[1], &testPost1111)) && unwrap(arePostsEqual(posts[0], &testPostNA))) {
		t.Fatalf("I'll write a better explanation, what's wrong, when something breaks, now I'm lazy :c")
	}
}

func TestDatabase_ChangePost(t *testing.T) {
	db = NewDatabase(redisTestingDatabaseKeyPrefix, redisTestingConfig)
	err := db.client.FlushDB(redisContext).Err()
	if err != nil {
		t.Fatal(err.Error())
	}

	_, err = db.AddPost(&testPostNA)
	if err != nil {
		t.Fatal(err.Error())
	}

	newPost, err := db.ChangePost(testPostNA.Id, ChangePostTime, "11:11")
	if err != nil {
		t.Fatal(err.Error())
	}

	equal, reason := arePostsEqual(newPost, &testPostNA)
	if equal {
		t.Fatal("PostExtended hadn't changed")
	}
	if reason != fmt.Sprintf("lhs.Time != rhs.Time, %s != %s", newPost.Time, testPostNA.Time) {
		t.Fatalf("reason of inequality if wrong: %s", reason)
	}
}
