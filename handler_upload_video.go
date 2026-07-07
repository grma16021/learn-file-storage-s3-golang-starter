package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<30)

	videoID := r.PathValue("videoID")
	parsedVideoID, err := uuid.Parse(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error parsing video ID", err)
		return
	}
	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "error getting bearer token", err)
		return
	}
	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "error validating JWT", err)
		return
	}

	videoMetaData, err := cfg.db.GetVideo(parsedVideoID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "error fetching video from db", err)
		return
	}
	if videoMetaData.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "error user ID's do not match", err)
		return
	}

	video, header, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error parsing video", err)
		return
	}
	defer video.Close()
	contentType := header.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "error parsing mime type", err)
		return
	}

	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "error parsing mime type", err)
	}

	temp, err := os.CreateTemp("", "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error creating temp file", err)
		return
	}

	defer os.Remove("tubely-upload.mp4")
	defer temp.Close()

	if _, err := io.Copy(temp, video); err != nil {
		respondWithError(w, http.StatusInternalServerError, "error copying video", err)
		return
	}

	if _, err := temp.Seek(0, io.SeekStart); err != nil {
		respondWithError(w, http.StatusInternalServerError, "error reseting pointer", err)
		return
	}

	key := make([]byte, 32)
	rand.Read(key)
	keySTR := base64.RawURLEncoding.EncodeToString(key)

	_, err = cfg.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket:      aws.String(cfg.s3Bucket),
		Key:         aws.String(keySTR),
		Body:        temp,
		ContentType: aws.String("video/mp4"),
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error uploading thing", err)
		return
	}

	videoURl := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", cfg.s3Bucket, cfg.s3Region, keySTR)

	videoMetaData.VideoURL = &videoURl
	cfg.db.UpdateVideo(videoMetaData)
}
