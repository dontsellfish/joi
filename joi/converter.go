package joi

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

const (
	DefaultConvertPath  = "convert"
	DefaultIdentifyPath = "identify"
	DefaultFfmpegPath   = "ffmpeg"
	DefaultMaximumSizes = "3840x3840"
	DefaultJpgQuality   = "95"
	DefaultPreset       = "fast"
)

type Converter struct {
	ConvertPath  string
	FfmpegPath   string
	IdentifyPath string

	MaximumSizes string
	JpgQuality   string
	Preset       string
}

func NewConverter(args ...Converter) *Converter {
	converter := Converter{}
	if len(args) > 0 {
		converter = args[0]
	}

	if converter.ConvertPath == "" {
		converter.ConvertPath = DefaultConvertPath
	}
	if converter.IdentifyPath == "" {
		converter.IdentifyPath = DefaultIdentifyPath
	}
	if converter.FfmpegPath == "" {
		converter.FfmpegPath = DefaultFfmpegPath
	}
	if converter.Preset == "" {
		converter.Preset = DefaultPreset
	}
	if converter.MaximumSizes == "" {
		converter.MaximumSizes = DefaultMaximumSizes
	}
	if converter.JpgQuality == "" {
		converter.JpgQuality = DefaultJpgQuality
	}

	return &converter
}

func (converter *Converter) IdentifyImage(filename string) (*ImageInfo, error) {
	stat, err := os.Stat(filename)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("while identifying %s an error occured %s", filename, err.Error()))
	}
	info := ImageInfo{Size: stat.Size()}

	output, err := exec.Command(converter.IdentifyPath, filename).Output()
	if err != nil {
		return nil, errors.New(fmt.Sprintf("while identifying %s an error occured %s\n%s", filename, err.Error(), string(output)))
	}
	args := strings.Split(string(output), " ")
	if len(args) < 3 {
		return nil, errors.New(fmt.Sprintf("while identifying %s, weird info has gotten %s", filename, string(output)))
	}

	info.Type = strings.ToLower(args[1])
	_, err = fmt.Sscanf(args[2], "%dx%d", &info.Sizes.Width, &info.Sizes.Height)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("while identifying %s an error occured %s\n%s", filename, err.Error(), string(output)))
	}

	return &info, nil
}

func (converter *Converter) Image(filename string) (string, error) {
	newImg := filename + ".jpg"
	output, err := exec.Command(converter.ConvertPath, "-strip", "-resize", converter.MaximumSizes, "-quality", converter.JpgQuality, filename, newImg).Output()
	if err != nil {
		return "", errors.New(fmt.Sprintf("while converting %s an error occured %s\n%s", filename, err.Error(), string(output)))
	}
	return newImg, nil
}

func (converter *Converter) ImageTelegram(filename string) (string, error) {
	quality, _ := strconv.ParseInt(converter.JpgQuality, 10, 64)
	for quality >= 80 {
		info, err := converter.IdentifyImage(filename)
		if err != nil {
			return "", err
		}
		if info.IsTooBigForTelegram() {
			newImg := filename + ".jpg"
			output, err := exec.Command(converter.ConvertPath, "-strip", "-resize", converter.MaximumSizes,
				"-quality", strconv.Itoa(int(quality)),
				filename, newImg).Output()
			if err != nil {
				return "", errors.New(fmt.Sprintf("while converting %s an error occured %s\n%s", filename, err.Error(), string(output)))
			}
			err = os.Rename(newImg, filename)
			if err != nil {
				return "", err
			}
			quality -= 5
		} else {
			return filename, err
		}
	}
	return "", errors.New("what the hell is broken with " + filename)
}

type ImageInfo struct {
	Size  int64
	Sizes struct {
		Height, Width int
	}
	Type string
}

func (info *ImageInfo) IsTooBigForTelegram() bool {
	if info.Size > 5_000_000 || info.Sizes.Width > 3840 || info.Sizes.Height > 3840 {
		return true
	}
	for _, ext := range []string{"png", "jpg", "jpeg"} {
		if ext == info.Type {
			return false
		}
	}
	return true
}

func (converter *Converter) Video(filename string) (string, error) {
	newVideo := filename + "_h264.mp4"
	output, err := exec.Command("ffmpeg", "-i", filename, "-vcodec", "libx264", "-acodec", "aac", "-y",
		"-preset", "fast", "-map_metadata", "-1", "-metadata", "meh=t.me/by_meh", newVideo).Output()
	if err != nil {
		return "", errors.New(fmt.Sprintf("while converting %s an error occured %s\n%s", filename, err.Error(), string(output)))
	}
	return newVideo, nil
}
