package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type User struct {
	ID       string `json:"id,omitempty" bson:"id,omitempty"`
	Name     string `json:"name,omitempty" bson:"name,omitempty"`
	Email    string `json:"email,omitempty" bson:"email,omitempty"`
	Password string `json:"password,omitempty" bson:"password,omitempty"`
}

type Post struct {
	ID               string `json:"id,omitempty" bson:"id,omitempty"`
	Caption          string `json:"caption,omitempty" bson:"caption,omitempty"`
	Image_URL        string `json:"image_url,omitempty" bson:"image_url,omitempty"`
	Posted_Timestamp string `json:"timestamp,omitempty" bson:"timestamp,omitempty"`
}

var (
	UserCollection *mongo.Collection
	PostCollection *mongo.Collection
	Ctx            = context.TODO()
)

var (
	getUserRe    = regexp.MustCompile(`^\/users\/(\d+)$`)
	createUserRe = regexp.MustCompile(`^\/users[\/]*$`)
	getAllPostRe = regexp.MustCompile(`^\/posts\/users\/(\d+)$`)
	getPostRe    = regexp.MustCompile(`^\/posts\/(\d+)$`)
	createPostRe = regexp.MustCompile(`^\/posts[\/]*$`)
)

type Handler struct {
	*sync.RWMutex
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("content-type", "application/json")
	switch {
	case r.Method == http.MethodGet && getUserRe.MatchString(r.URL.Path):
		h.Get(w, r)
		return
	case r.Method == http.MethodPost && createUserRe.MatchString(r.URL.Path):
		h.Create(w, r)
		return
	case r.Method == http.MethodPost && createPostRe.MatchString(r.URL.Path):
		h.CreatePost(w, r)
		return
	case r.Method == http.MethodGet && getPostRe.MatchString(r.URL.Path):
		h.GetPost(w, r)
		return
	case r.Method == http.MethodGet && getAllPostRe.MatchString(r.URL.Path):
		h.GetAllPosts(w, r)
		return
	}
}
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var u User
	if err := json.NewDecoder(r.Body).Decode(&u); err != nil {

		return
	}
	h.Lock()
	plaintext := u.Password
	u.Password = string(encrypt([]byte(plaintext), "password"))
	w.Header().Set("content-type", "application/json")

	_ = json.NewDecoder(r.Body).Decode(&u)
	fmt.Println(u)
	result, err := CreateUser(u)
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println(result)
	h.Unlock()
	w.WriteHeader(http.StatusOK)

}

func (h *Handler) CreatePost(w http.ResponseWriter, r *http.Request) {
	var p Post
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {

		return
	}
	h.Lock()
	w.Header().Set("content-type", "application/json")

	_ = json.NewDecoder(r.Body).Decode(&p)
	p.Posted_Timestamp = time.Now().Format(time.RFC3339)
	fmt.Println(p)
	result, err := CreateDbPost(p)
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println(result)
	h.Unlock()
	w.WriteHeader(http.StatusOK)

}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.String(), "/")
	if len(parts) != 3 {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	h.RLock()
	u, err := GetUser(parts[2])
	ciphertext := u.Password
	u.Password = string(decrypt([]byte(ciphertext), "password"))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{ "message": "` + err.Error() + `" }`))
		return
	}
	json.NewEncoder(w).Encode(u)
	h.RUnlock()
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) GetAllPosts(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.String(), "/")

	if len(parts) != 4 {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	h.RLock()
	p, err := GetDbPosts(parts[3])

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{ "message": "` + err.Error() + `" }`))
		return
	}
	json.NewEncoder(w).Encode(p)
	h.RUnlock()
	w.WriteHeader(http.StatusOK)
}

func GetDbPosts(id string) ([]Post, error) {
	var post Post
	var userposts []Post

	cursor, err := PostCollection.Find(Ctx, bson.D{{"id", id}})
	if err != nil {
		defer cursor.Close(Ctx)
		return userposts, err
	}

	for cursor.Next(Ctx) {
		err := cursor.Decode(&post)
		if err != nil {
			return userposts, err
		}
		userposts = append(userposts, post)
	}

	return userposts, nil
}
func (h *Handler) GetPost(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.String(), "/")

	if len(parts) != 3 {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	h.RLock()
	u, err := GetDbPost(parts[2])
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{ "message": "` + err.Error() + `" }`))
		return
	}
	json.NewEncoder(w).Encode(u)
	h.RUnlock()

	w.WriteHeader(http.StatusOK)

}

/*Setup opens a database connection to mongodb*/
func Setup() {
	host := "127.0.0.1"
	port := "27017"
	connectionURI := "mongodb://" + host + ":" + port + "/"
	clientOptions := options.Client().ApplyURI(connectionURI)
	client, err := mongo.Connect(Ctx, clientOptions)
	if err != nil {
		log.Fatal(err)
	}

	err = client.Ping(Ctx, nil)
	if err != nil {
		log.Fatal(err)
	}

	db := client.Database("Instagram")
	UserCollection = db.Collection("Users")
	PostCollection = db.Collection("Posts")
}
func CreateUser(u User) (string, error) {
	result, err := UserCollection.InsertOne(Ctx, u)
	if err != nil {
		return "0", err
	}
	return fmt.Sprintf("%v", result.InsertedID), err
}

func GetUser(id string) (User, error) {
	var u User
	err := UserCollection.
		FindOne(Ctx, bson.D{{"id", id}}).
		Decode(&u)

	if err != nil {
		return u, err
	}
	return u, nil
}
func CreateDbPost(p Post) (string, error) {
	result, err := PostCollection.InsertOne(Ctx, p)
	if err != nil {
		return "0", err
	}
	return fmt.Sprintf("%v", result.InsertedID), err
}

func GetDbPost(id string) (Post, error) {
	var p Post
	fmt.Println(id)
	err := PostCollection.
		FindOne(Ctx, bson.D{{"id", id}}).
		Decode(&p)

	if err != nil {
		return p, err
	}
	return p, nil
}
func createHash(key string) string {
	hasher := md5.New()
	hasher.Write([]byte(key))
	return hex.EncodeToString(hasher.Sum(nil))
}

func encrypt(data []byte, passphrase string) []byte {
	block, _ := aes.NewCipher([]byte(createHash(passphrase)))
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		panic(err.Error())
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		panic(err.Error())
	}
	ciphertext := gcm.Seal(nonce, nonce, data, nil)
	return ciphertext
}

func decrypt(data []byte, passphrase string) []byte {
	key := []byte(createHash(passphrase))
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err.Error())
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		panic(err.Error())
	}
	nonceSize := gcm.NonceSize()
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		panic(err.Error())
	}
	return plaintext
}
func main() {
	Setup()
	mux := http.NewServeMux()
	handler := &Handler{
		RWMutex: &sync.RWMutex{},
	}
	mux.Handle("/users", handler)
	mux.Handle("/users/", handler)
	mux.Handle("/posts", handler)
	mux.Handle("/posts/", handler)

	http.ListenAndServe("localhost:9094", mux)
}
