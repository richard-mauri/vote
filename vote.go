package main

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
	Secret   = "secret" // TODO: Hardening
	VoteHtml = "vote.html"
)

// User is a type of object that will be associated with a Jwt Claim (for authorized voting)
type User struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// JwtToken is a type of object that encapsulates a Jwt token, gets JSON encoded and returned to the registered voter
type JwtToken struct {
	Token string `json:"token"`
}

// Feedback is a type of object used to encapsulate a message to be logged
type Feedback struct {
	Message string `json:"message"`
}

// InputVote is a type of object used to associate a voter and the candidate he voted for
type InputVote struct {
	Username  string
	Candidate string
}

// TokenParser is an interface that facilitates testing and decoupling mock vs Jwt token parsing
type TokenParser interface {
	TokenParse() (username string, err error)
}

// JwtTokenParser is a type of object used for concrete Jwt token parsing (a receiver type)
type JwtTokenParser struct {
	token string
}

// TokenParse is a concrete Jwt based implementation of a TokenParser used to extract username from a Jwt token
func (p *JwtTokenParser) TokenParse() (username string, err error) {
	token, err := jwt.Parse(p.token, func(token *jwt.Token) (interface{}, error) {
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

// NewJwtTokenParser acts as a constructor for a JwtTokenParser
func NewJwtTokenParser(token string) (parser TokenParser) {
	return &JwtTokenParser{token: token}
}

var (
	politeTokenError = fmt.Errorf("Please provide a valid JWT token")
	VersionString    = ""
	voteAddr         *string
	redisAddr        *string
	redisClient      *redis.Client      // connections are goroutine safe
	tmpl             *template.Template // goroutine safe
	candidates       = []string{        // TODO: Encapsulate as a pluggable Ballot
		"JoeBiden",
		"BetoORourke",
		"BernieSanders",
		"ElizabethWarren",
		"KamalaHarris",
		"DonaldTrump"}
)

// validateMiddleware is an Http handler midleware wrapper (common code) for processing a Jwt token
// A valid Jwt token claim will be injectd into a context object for use by the next chained handler
// TODO: Normalize against the voteUIHandler and TokenParser
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

// credkey constructs a key used to identify a user's credentials
func credkey(username string) string {
	return username + ".cred"
}

// validUsername validates that a voter (aka user) is identified by an email string
func validUsername(username string) bool {
	re := regexp.MustCompile(`^[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,4}$`)

	return re.MatchString(username)
}

// registerHandler is an http handler that will store a voter/user credential and construct a Jwt token for use in subsequent voting operations
func registerHandler(w http.ResponseWriter, r *http.Request) {
	var user User
	err := json.NewDecoder(r.Body).Decode(&user)
	if err != nil {
		log.Println("Error decoding user: " + err.Error())
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	tokenString, err := createJwtClaim(user.Username, user.Password)

	if err != nil {
		log.Println("Error creating Jwt claim: " + err.Error())
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	err = redisClient.Set(credkey(user.Username), user.Password, 0).Err()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(JwtToken{Token: tokenString})
}

func createJwtClaim(username, password string) (tokenString string, err error) {
	if !validUsername(username) || password == "" {
		err = fmt.Errorf("Illegal username or password")
		return tokenString, err
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"username": username,
		"password": password,
	})
	return token.SignedString([]byte(Secret))
}

// setVoteHandler is an http handler that will cast a voter/user's vote for a candidate
// It must be called by middleware to set the Jwt Claim
// Only one vorte by a voter is allowed
func setVoteHandler(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	candidate := params["candidate"]

	decoded := context.Get(r, "decoded")
	var user User
	mapstructure.Decode(decoded.(jwt.MapClaims), &user)
	username := user.Username

	log.Println("setVoteHandler: username = " + username + " ; candidate = " + candidate)

	// Be double safe. Perform JWT verify AND confirm credentials match what was stored in the DB at registration time
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

// voteUIHandler is an http handler that expects an Jwt token (represents a voter) and a candiate to be supplied as form data
// TODO: Normalize against the setVoteHandler which currently expects this input data to be in an Http Header
func voteUIHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		tmpl.Execute(w, nil)
		return
	}

	jwt := r.FormValue("jwt")
	tp := NewJwtTokenParser(jwt)
	username, err := tp.TokenParse()

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

// saveVote will cast a voter's vote only if not done previously
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

// helpHandler is an http handler for printing the http endpoint signatures
func helpHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	msg := "POST /register Body like: {'username': 'xxx', 'password': 'yyy'} (returns a JWT token)\n"
	msg += "POST /vote/{candidate} Heder like: 'authorization: jwt xxx.yyy.zzz' (one vote per user)\n"
	msg += "GET /vote (shows current votes per candidate)\n"
	w.Write([]byte(msg))
}

// getVotesHandler is an http handler that will present a JSON encoded tally of all the candidate votes
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

// getCandidatesVotes is a helper func that queries the DB for the candidate votes
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

// hasAlreadyVoted is a helper func that queries the DB to chck that a voter/user has not already voted
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

// newRedisClient is a constructor for a Redis client which acts as our voting DB
func newRedisClient() *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:     *redisAddr,
		Password: "", // TODO: hardening
		DB:       0,
	})
}

// genVotingHtmlForm creates an html file with form data for a voter to cast a vote
func genVotingHtmlForm() error {
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
	err := ioutil.WriteFile(VoteHtml, message, 0644)
	return err
}

// parseFlags parses CLI flags to facilitate configurability
func parseFlags() {
	version := flag.Bool("v", false, "print current version and exit")
	voteAddr = flag.String("voteAddr", ":8000", "vote address")
	redisAddr = flag.String("redisAddr", "redis:6379", "redis address")

	flag.Parse()

	if *version {
		fmt.Println(VersionString)
		os.Exit(0)
	}
}

// The main entry point for the vote server
func main() {
	parseFlags()

	err := genVotingHtmlForm()
	if err != nil {
		log.Fatal(err)
	}

	tmpl = template.Must(template.ParseFiles(VoteHtml))

	redisClient = newRedisClient()
	router := mux.NewRouter()

	router.HandleFunc("/help", helpHandler)
	router.HandleFunc("/register", registerHandler).Methods("POST")
	router.HandleFunc("/vote/{candidate}", validateMiddleware(setVoteHandler)).Methods("POST")
	router.HandleFunc("/vote", getVotesHandler).Methods("GET")
	router.HandleFunc("/voteui", voteUIHandler)
	router.PathPrefix("/").Handler(http.FileServer(http.Dir("."))) // For serving static content like vote.jpg

	log.Fatal(http.ListenAndServe(*voteAddr, router))
}
