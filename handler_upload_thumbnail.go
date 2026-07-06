package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
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

	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	const maxMemory = 10 << 20
	r.ParseMultipartForm(maxMemory)

	file, header, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}

	defer file.Close()

	videoKey := make([]byte, 32)
	rand.Read(videoKey)
	videoKeySTR := base64.RawURLEncoding.EncodeToString(videoKey)

	fileExt := header.Header.Get("Content-Type")
	contentType := strings.Split(fileExt, "/")
	fileThing := videoKeySTR + "." + contentType[1]
	log.Printf("THING: %s", fileThing)
	//imageData, err := io.ReadAll(file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to parse image", err)
		return
	}

	mType, _, err := mime.ParseMediaType(fileExt)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error parsing media type", err)
		return
	}
	if mType != "image/jpeg" && mType != "image/png" {
		respondWithError(w, http.StatusBadRequest, "error wrong file type", err)
		return
	}

	fPath := filepath.Join(cfg.assetsRoot, fileThing)

	newFile, err := os.Create(fPath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error creating new file: %s", err)
		return
	}

	io.Copy(newFile, file)

	videoMetaData, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error getting video from db", err)
	}

	if videoMetaData.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "error user id's do not match", err)
	}

	//thumbNailURL := fmt.Sprintf("http://localhost:8091/api/thumbnails/%s", videoID)
	//var thumbPtr *string = &thumbNailURL

	//encodedThumbURL := base64.StdEncoding.EncodeToString([]byte(imageData))
	thumbnailDataURL := fmt.Sprintf("http://localhost:8091/assets/%s.%s", videoKeySTR, contentType[1])
	log.Printf("URL: %s", thumbnailDataURL)

	videoMetaData.ThumbnailURL = &thumbnailDataURL

	err = cfg.db.UpdateVideo(videoMetaData)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error updating video", err)
	}

	respondWithJSON(w, http.StatusOK, videoMetaData)
}
