package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"os/exec"

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

	if _, err := io.Copy(temp, video); err != nil {
		respondWithError(w, http.StatusInternalServerError, "error copying video", err)
		return
	}

	if _, err := temp.Seek(0, io.SeekStart); err != nil {
		respondWithError(w, http.StatusInternalServerError, "error reseting pointer", err)
		return
	}

	orientation, err := getVideoAspectRatio(temp.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error running ffmpeg", err)
	}

	processedVideo, err := processVideoForFastStart(temp.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error processing video", err)
		return
	}

	processedFile, err := os.Open(processedVideo)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error opening processed file", err)
		return
	}

	defer os.Remove(processedVideo)
	defer processedFile.Close()

	defer os.Remove("tubely-upload.mp4")
	defer temp.Close()

	key := make([]byte, 32)
	rand.Read(key)
	log.Printf("orientation: %s", orientation)
	keySTR := fmt.Sprintf("/%s/", orientation) + base64.RawURLEncoding.EncodeToString(key)

	_, err = cfg.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket:      aws.String(cfg.s3Bucket),
		Key:         aws.String(keySTR),
		Body:        processedFile,
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

func getVideoAspectRatio(filePath string) (string, error) {

	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)

	var videoBytes bytes.Buffer
	var errorBytes bytes.Buffer
	cmd.Stdout = &videoBytes
	cmd.Stderr = &errorBytes
	err := cmd.Run()
	log.Printf("command finished with error: %v", errorBytes.String())

	type streams struct {
		Heigth int `json:"height"`
		Width  int `json:"width"`
	}

	type videoJSON struct {
		Streams []streams `json:"streams"`
	}
	var videoJson videoJSON

	err = json.Unmarshal(videoBytes.Bytes(), &videoJson)
	if err != nil {
		return "", fmt.Errorf("error unmarshaling ffprobe json: %s", err)

	}
	var aspectRatio float64
	for _, thing := range videoJson.Streams {
		aspectRatio = float64(thing.Width) / float64(thing.Heigth)
		log.Printf("width: %d, height: %d, aspectRatio: %f", thing.Width, thing.Heigth, aspectRatio)
		break
	}

	var orientation string
	if aspectRatio >= 1.6 && aspectRatio <= 1.8 {
		orientation = "landscape"
		if orientation == "" {
			return "", fmt.Errorf("error orientation is empty")
		}
	} else if aspectRatio >= 0.4 && aspectRatio <= 0.6 {
		orientation = "portrait"
		if orientation == "" {
			return "", fmt.Errorf("error orientation is empty")
		}
	} else {
		orientation = "other"
	}

	return orientation, nil

}

func processVideoForFastStart(filePath string) (string, error) {
	outputFilePath := filePath + ".processing"

	cmd := exec.Command("ffmpeg", "-i", filePath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", outputFilePath)

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("error running command ")
	}

	return outputFilePath, nil
}
