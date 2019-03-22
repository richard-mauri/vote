package main

// See: https://www.thepolyglotdeveloper.com/2017/03/authenticate-a-golang-api-with-json-web-tokens/

import (
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/dgrijalva/jwt-go"
	"github.com/go-redis/redis"
	"github.com/gorilla/context"
	"github.com/gorilla/mux"
	"github.com/mitchellh/mapstructure"
)

const (
	Secret = "secret" // TODO: Hardening
)

type User struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type JwtToken struct {
	Token string `json:"token"`
}

type Feedback struct {
	Message string `json:"message"`
}

type InputVote struct {
	Username  string
	Candidate string
}

var (
	politeTokenError = fmt.Errorf("Please provide a valid JWT token")
	VersionString    = ""
	redisClient      *redis.Client      // connections are goroutine safe
	tmpl             *template.Template // goroutine safe
	candidates       = []string{
		"JoeBiden",
		"BetoORourke",
		"BernieSanders",
		"ElizabethWarren",
		"KamalaHarris",
		"DonaldTrump"}
)

func getUsernameFromToken(inputjwt string) (username string, err error) {
	if inputjwt == "" {
		err = politeTokenError
		return username, err
	}
	token, err := jwt.Parse(inputjwt, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, politeTokenError
		}
		return []byte(Secret), nil
	})
	if err != nil {
		log.Println("Error processing JWT token: " + err.Error())
		err = politeTokenError
		return username, err
	}
	if token.Valid {
		var user User
		mapstructure.Decode(token.Claims, &user)
		username = user.Username
	}
	return username, err
}

func validateMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		authorizationHeader := req.Header.Get("authorization")
		if authorizationHeader != "" {
			bearerToken := strings.Split(authorizationHeader, " ")
			if len(bearerToken) == 2 {
				token, error := jwt.Parse(bearerToken[1], func(token *jwt.Token) (interface{}, error) {
					if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
						return nil, fmt.Errorf("Error processing the JWT token")
					}
					return []byte(Secret), nil
				})
				if error != nil {
					json.NewEncoder(w).Encode(Feedback{Message: error.Error()})
					return
				}
				if token.Valid {
					context.Set(req, "decoded", token.Claims)
					next(w, req)
				} else {
					json.NewEncoder(w).Encode(Feedback{Message: "Invalid authorization token"})
				}
			} else {
				json.NewEncoder(w).Encode(Feedback{Message: "An authorization header with two components was not supplied"})
			}
		} else {
			json.NewEncoder(w).Encode(Feedback{Message: "An authorization header is required"})
		}
	})
}

func credkey(username string) string {
	return username + ".cred"
}

func validUsername(username string) bool {
	re := regexp.MustCompile(
		"^[a-zA-Z0-9.!#$%&'*+/=?^_`{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$")

	return re.MatchString(username)
}

func registerHandler(w http.ResponseWriter, r *http.Request) {
	var user User
	err := json.NewDecoder(r.Body).Decode(&user)
	if err != nil {
		log.Println("Error decoding user: " + err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if !validUsername(user.Username) || user.Password == "" {
		log.Println("Illegal username or password")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	err = redisClient.Set(credkey(user.Username), user.Password, 0).Err()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"username": user.Username,
		"password": user.Password,
	})
	tokenString, err := token.SignedString([]byte(Secret))
	if err != nil {
		log.Println("Error signing secret : " + err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(JwtToken{Token: tokenString})
}

func setVoteHandler(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	candidate := params["candidate"]

	decoded := context.Get(r, "decoded")
	var user User
	mapstructure.Decode(decoded.(jwt.MapClaims), &user)
	username := user.Username

	log.Println("setVoteHandler: username = " + username + " ; candidate = " + candidate)

	// Be double safe! Perform JWT verify AND confirm credentials match what was stored in the DB at registration time
	_, err := redisClient.Get(credkey(username)).Result()
	if err == redis.Nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	validCandidate := false
	for _, c := range candidates {
		if c == candidate {
			validCandidate = true
		}
	}

	if !validCandidate {
		log.Println("Invalid candidate : " + candidate)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	input := InputVote{
		Username:  username,
		Candidate: candidate,
	}

	alreadyVoted, err := saveVote(input)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if alreadyVoted {
		msg := "User " + username + " has already voted"
		log.Println(msg)
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(msg))
		return
	}

	w.WriteHeader(http.StatusOK)
}

func voteUIHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		tmpl.Execute(w, nil)
		return
	}

	jwt := r.FormValue("jwt")
	username, err := getUsernameFromToken(jwt)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		tmpl.Execute(w, struct{ Status string }{err.Error()})
		return
	}

	input := InputVote{
		Username:  username,
		Candidate: r.FormValue("candidate"),
	}

	alreadyVoted, err := saveVote(input)
	if err != nil {
		fmt.Printf("Vote failure: %+v : %v\n", input, err)
		tmpl.Execute(w, struct{ Status string }{err.Error()})
	} else if alreadyVoted {
		tmpl.Execute(w, struct{ Status string }{"Already Voted"})
	} else {
		tmpl.Execute(w, struct{ Status string }{"Success"})
	}
}

func saveVote(inputVote InputVote) (alreadyVoted bool, err error) {
	alreadyVoted, err = hasAlreadyVoted(inputVote.Username)
	if err != nil || alreadyVoted {
		return alreadyVoted, err
	}

	err = redisClient.Incr(inputVote.Candidate).Err()
	if err != nil {
		return alreadyVoted, err
	}

	err = redisClient.Incr(inputVote.Username).Err()
	if err != nil {
		return alreadyVoted, err
	}
	return alreadyVoted, err
}

func helpHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	msg := "POST /register Body like: {'username': 'xxx', 'password': 'yyy'} (returns a JWT token)\n"
	msg += "POST /vote/{candidate} Heder like: 'authorization: jwt xxx.yyy.zzz' (one vote per user)\n"
	msg += "GET /vote (shows current votes per candidate)\n"
	w.Write([]byte(msg))
}

func getVotesHandler(w http.ResponseWriter, r *http.Request) {
	cmap, err := getCandidatesVotes()

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")

	enc := json.NewEncoder(w)
	enc.Encode(cmap)
}

func getCandidatesVotes() (cmap map[string]string, err error) {

	cmap = make(map[string]string)

	cvotesIF, err := redisClient.MGet(candidates...).Result()
	if err != nil {
		return cmap, err
	}

	for i, cif := range cvotesIF {
		cvotes, ok := cif.(string)
		if ok == false {
			cmap[candidates[i]] = "0"
		} else {
			cmap[candidates[i]] = cvotes
		}
	}
	return cmap, err
}

func hasAlreadyVoted(username string) (bool, error) {
	usernameCountStr, err := redisClient.Get(username).Result()
	if err == redis.Nil {
		return false, nil
	}

	if err != nil {
		if err == redis.Nil {
			return false, nil
		}
		return false, err
	}

	usernameCount, err := strconv.Atoi(usernameCountStr)
	if err != nil {
		return false, err
	}

	if usernameCount > 0 {
		return true, nil
	}
	return false, nil
}

func newRedisClient() *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:     "redis:6379",
		Password: "",
		DB:       0,
	})
}

func genHtmlForm() error {
	form := "<!DOCTYPE html>\n"
	form += "<html>\n"
	form += "<body>\n"
	form += "<img src=\"vote.jpg\" alt=\"Vote!\" height=\"182\" width=\"390\">\n"
	form += "<h1>Let's Vote!</h1>\n"
	form += "<form method=\"POST\">\n"
	form += "<label>Enter your JWT Token: </label>\n"
	form += "<input type=\"text\" name=\"jwt\" autocomplete=\"off\"><br/><br/>\n"
	form += "<label>The candidates:</label><br/>\n"

	for i, c := range candidates {
		line := fmt.Sprintf("<input type=\"radio\" name=\"candidate\" value=\"%s\"", c)
		if i == 0 {
			line += " checked"
		}
		line += "> "
		line += c
		line += "<br/>\n"
		form += line
	}

	form += "<br/><input type=\"submit\" value=\"Place your vote\"><br/><br/>\n"
	form += "<label>{{.Status}}</label>\n"
	form += "</form>\n"
	form += "</body>\n"
	form += "</html>"

	message := []byte(form)
	err := ioutil.WriteFile("vote.html", message, 0644)
	return err
}

func parseFlags() {
	version := flag.Bool("v", false, "print current version and exit")

	flag.Parse()

	if *version {
		fmt.Println(VersionString)
		os.Exit(0)
	}
}

func main() {
	parseFlags()

	err := genHtmlForm()
	if err != nil {
		log.Fatal(err)
	}

	tmpl = template.Must(template.ParseFiles("vote.html"))

	redisClient = newRedisClient()
	router := mux.NewRouter()

	router.HandleFunc("/help", helpHandler)
	router.HandleFunc("/register", registerHandler).Methods("POST")
	router.HandleFunc("/vote/{candidate}", validateMiddleware(setVoteHandler)).Methods("POST")
	router.HandleFunc("/vote", getVotesHandler).Methods("GET")
	router.HandleFunc("/voteui", voteUIHandler)
	router.PathPrefix("/").Handler(http.FileServer(http.Dir("."))) // For serving static content like vote.jpg

	log.Fatal(http.ListenAndServe(":8000", router))
}
