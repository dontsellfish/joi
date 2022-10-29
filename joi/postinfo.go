package joi

const (
	PostSourcesAuto = iota
	PostSourcesTrue
	PostSourcesFalse
)

const (
	TelegramFileTypePhoto = iota
	TelegramFileTypeVideo
	TelegramFileTypeDocPhoto
	TelegramFileTypeDocVideo
)

const (
	ChangePostTime = iota
	ChangePostText
	ChangePostComment
	ChangePostPostSources
	ChangePostIsProtected
	ChangePostMsgIdInCommentsChat
)

const TimeIsNotSpecified = "NA"

type TgFileInfo struct {
	Type int
	Id   string
}

type PostInfo struct {
	Id                  string
	Time                string
	Text                string
	Comment             string
	PostSources         int
	IsProtected         bool
	Files               []TgFileInfo
	MsgIdInCommentsChat int
	AdminPostedId       int64
	OriginalMsgIds      []int64
}
