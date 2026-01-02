package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/database"
)

type VideoData struct {
	Streams []struct {
		Index              int    `json:"index"`
		CodecName          string `json:"codec_name,omitempty"`
		CodecLongName      string `json:"codec_long_name,omitempty"`
		Profile            string `json:"profile,omitempty"`
		CodecType          string `json:"codec_type"`
		CodecTagString     string `json:"codec_tag_string"`
		CodecTag           string `json:"codec_tag"`
		Width              int    `json:"width,omitempty"`
		Height             int    `json:"height,omitempty"`
		CodedWidth         int    `json:"coded_width,omitempty"`
		CodedHeight        int    `json:"coded_height,omitempty"`
		HasBFrames         int    `json:"has_b_frames,omitempty"`
		SampleAspectRatio  string `json:"sample_aspect_ratio,omitempty"`
		DisplayAspectRatio string `json:"display_aspect_ratio,omitempty"`
		PixFmt             string `json:"pix_fmt,omitempty"`
		Level              int    `json:"level,omitempty"`
		ColorRange         string `json:"color_range,omitempty"`
		ColorSpace         string `json:"color_space,omitempty"`
		ColorTransfer      string `json:"color_transfer,omitempty"`
		ColorPrimaries     string `json:"color_primaries,omitempty"`
		ChromaLocation     string `json:"chroma_location,omitempty"`
		FieldOrder         string `json:"field_order,omitempty"`
		Refs               int    `json:"refs,omitempty"`
		IsAvc              string `json:"is_avc,omitempty"`
		NalLengthSize      string `json:"nal_length_size,omitempty"`
		ID                 string `json:"id"`
		RFrameRate         string `json:"r_frame_rate"`
		AvgFrameRate       string `json:"avg_frame_rate"`
		TimeBase           string `json:"time_base"`
		StartPts           int    `json:"start_pts"`
		StartTime          string `json:"start_time"`
		DurationTs         int    `json:"duration_ts"`
		Duration           string `json:"duration"`
		BitRate            string `json:"bit_rate,omitempty"`
		BitsPerRawSample   string `json:"bits_per_raw_sample,omitempty"`
		NbFrames           string `json:"nb_frames"`
		ExtradataSize      int    `json:"extradata_size"`
		Disposition        struct {
			Default         int `json:"default"`
			Dub             int `json:"dub"`
			Original        int `json:"original"`
			Comment         int `json:"comment"`
			Lyrics          int `json:"lyrics"`
			Karaoke         int `json:"karaoke"`
			Forced          int `json:"forced"`
			HearingImpaired int `json:"hearing_impaired"`
			VisualImpaired  int `json:"visual_impaired"`
			CleanEffects    int `json:"clean_effects"`
			AttachedPic     int `json:"attached_pic"`
			TimedThumbnails int `json:"timed_thumbnails"`
			NonDiegetic     int `json:"non_diegetic"`
			Captions        int `json:"captions"`
			Descriptions    int `json:"descriptions"`
			Metadata        int `json:"metadata"`
			Dependent       int `json:"dependent"`
			StillImage      int `json:"still_image"`
			Multilayer      int `json:"multilayer"`
		} `json:"disposition"`
		Tags struct {
			Language    string `json:"language"`
			HandlerName string `json:"handler_name"`
			VendorID    string `json:"vendor_id"`
			Encoder     string `json:"encoder"`
			Timecode    string `json:"timecode"`
		} `json:"tags,omitempty"`
		SampleFmt      string `json:"sample_fmt,omitempty"`
		SampleRate     string `json:"sample_rate,omitempty"`
		Channels       int    `json:"channels,omitempty"`
		ChannelLayout  string `json:"channel_layout,omitempty"`
		BitsPerSample  int    `json:"bits_per_sample,omitempty"`
		InitialPadding int    `json:"initial_padding,omitempty"`
		Tags0          struct {
			Language    string `json:"language"`
			HandlerName string `json:"handler_name"`
			VendorID    string `json:"vendor_id"`
		} `json:"tags,omitempty"`
		Tags1 struct {
			Language    string `json:"language"`
			HandlerName string `json:"handler_name"`
			Timecode    string `json:"timecode"`
		} `json:"tags,omitempty"`
	} `json:"streams"`
}

func roundRatio(num float64) float64 {
	return float64(int(num*100)) / 100
}

func getVideoAspectRatio(filePath string) (string, error) {
	var b bytes.Buffer

	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
	cmd.Stdout = &b

	if err := cmd.Run(); err != nil {
		return "", err
	}

	res := VideoData{}
	if err := json.Unmarshal(b.Bytes(), &res); err != nil {
		return "", err
	}

	verticalRatio := roundRatio(float64(9) / float64(16))
	horizontalRatio := roundRatio(float64(float64(16) / (9)))

	width, height := res.Streams[0].Width, res.Streams[0].Height
	ratio := roundRatio(float64(width) / float64(height))

	switch ratio {
	case verticalRatio:
		return "portrait", nil
	case horizontalRatio:
		return "landscape", nil
	default:
		return "other", nil
	}
}

func processVideoForFastStart(filePath string) (string, error) {
	newOut := fmt.Sprintf("%s.processing", filePath)

	cmd := exec.Command("ffmpeg", "-i", filePath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", newOut)
	if err := cmd.Run(); err != nil {
		return "", err
	}

	return newOut, nil
}

func generatePresignedURL(s3Client *s3.Client, bucket, key string, expireTime time.Duration) (string, error) {

	presignClient := s3.NewPresignClient(s3Client)

	params := s3.GetObjectInput{
		Bucket: &bucket,
		Key:    &key,
	}
	presignOptions := s3.WithPresignExpires(expireTime)

	presignedRequest, err := presignClient.PresignGetObject(context.Background(), &params, presignOptions)
	if err != nil {
		return "", err
	}

	return presignedRequest.URL, nil
}

func (cfg *apiConfig) dbToSignedVideo(video database.Video) (string, error) {
	if video.VideoURL == nil {
		return "", fmt.Errorf("Invalid input, missing VideoURL field in video with id: %s", video.ID)
	}
	urlData := strings.Split(*video.VideoURL, ",")
	if len(urlData) != 2 {
		return "", fmt.Errorf("Invalid input, could not split a video url: %s, got %v", *video.VideoURL, urlData)
	}
	return generatePresignedURL(cfg.s3Client, urlData[0], urlData[1], cfg.presignedVideoExpiration)
}

func (cfg *apiConfig) deleteS3Video(video database.Video) error {
	if video.VideoURL != nil {
		videoName := getAssetPathByUrl(video.VideoURL)
		deleteParams := s3.DeleteObjectInput{
			Bucket: &cfg.s3Bucket,
			Key:    &videoName,
		}
		_, err := cfg.s3Client.DeleteObject(context.Background(), &deleteParams)
		return err
	}
	return nil
}

func (cfg *apiConfig) deleteVideo(video database.Video) error {
	err := cfg.deleteS3Video(video)
	if err != nil {
		return err
	}
	return cfg.db.DeleteVideo(video.ID)
}
