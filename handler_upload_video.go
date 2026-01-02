package main

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {

	const maxUploadLimit = 1 << 30
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadLimit)

	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "Couldn't find a video", err)
		return
	}

	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "User does not have permission to upload a video", nil)
		return
	}

	file, meta, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't get a video", err)
		return
	}
	defer file.Close()

	mediaType := meta.Header.Get("Content-Type")
	if mediaType == "" {
		respondWithError(w, http.StatusBadRequest, "Missing Content-Type for video", nil)
		return
	}

	mediaType, _, err = mime.ParseMediaType(mediaType)

	if mediaType == "" {
		respondWithError(w, http.StatusBadRequest, "Missing Content-Type for video", nil)
		return
	}

	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "Invalid media type for video, needs to be mp4", nil)
		return
	}

	tmpFile, err := os.CreateTemp("", "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Could not create a temprory file for a video", err)
		return
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	_, err = io.Copy(tmpFile, file)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Could not copy a temprory file for a video", err)
		return
	}
	_, err = tmpFile.Seek(0, io.SeekStart)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Could reset a pointer to temprory file for a video", err)
		return
	}

	aspectRatio, err := getVideoAspectRatio(tmpFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Could not get a video ratio", err)
		return
	}

	processedVideoPath, err := processVideoForFastStart(tmpFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Could not pre process video for fast start", err)
		return
	}
	processedVideo, err := os.Open(processedVideoPath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Could not open pre processed video", err)
		return
	}
	defer os.Remove(processedVideo.Name())
	defer processedVideo.Close()

	videoKey := fmt.Sprintf("%s/%s", aspectRatio, getAssetPath(mediaType))
	params := s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &videoKey,
		ContentType: &mediaType,
		Body:        processedVideo,
	}
	_, err = cfg.s3Client.PutObject(r.Context(), &params)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Could upload a video to s3", err)
		return
	}

	if err := cfg.deleteS3Video(video); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Could delete a previous video in s3", err)
		return
	}

	videoURL := fmt.Sprintf("%s,%s", cfg.s3Bucket, videoKey)
	video.VideoURL = &videoURL
	cfg.db.UpdateVideo(video)

	presignedURL, err := cfg.dbToSignedVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Could not get a presigned video", err)
		return
	}
	video.VideoURL = &presignedURL
	respondWithJSON(w, http.StatusOK, video)
}
