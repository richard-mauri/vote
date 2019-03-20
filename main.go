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
	"strconv"

	"github.com/go-redis/redis"
	"github.com/gorilla/mux"
)

type InputVote struct {
	VoterEmail string
	Candidate  string
}

var (
	VersionString = ""
	redisClient   *redis.Client      // connections are goroutine safe
	tmpl          *template.Template // goroutine safe
	candidates    = []string{
		"JoeBiden",
		"BetoORourke",
		"BernieSanders",
		"ElizabethWarren",
		"KamalaHarris",
		"DonaldTrump"}
)

func setVoteHandler(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)

	email := params["email"]
	candidate := params["candidate"]

	log.Println("setVoteHandler: email = " + email + " ; candidate = " + candidate)

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
		VoterEmail: email,
		Candidate:  candidate,
	}

	alreadyVoted, err := saveVote(input)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if alreadyVoted {
		log.Println("Already voted : " + email)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func voteUIHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		tmpl.Execute(w, nil)
		return
	}

	input := InputVote{
		VoterEmail: r.FormValue("email"),
		Candidate:  r.FormValue("candidate"),
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
	log.Println("saveVote: email = " + inputVote.VoterEmail + " ; candidate = " + inputVote.Candidate)

	alreadyVoted, err = hasAlreadyVoted(inputVote.VoterEmail)
	if err != nil || alreadyVoted {
		return alreadyVoted, err
	}

	err = redisClient.Incr(inputVote.Candidate).Err()
	if err != nil {
		log.Println("candidate incr err = " + err.Error())
		return alreadyVoted, err
	}

	err = redisClient.Incr(inputVote.VoterEmail).Err()
	if err != nil {
		log.Println("email incr err = " + err.Error())
		return alreadyVoted, err
	}
	return alreadyVoted, err
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

func hasAlreadyVoted(email string) (bool, error) {
	emailCountStr, err := redisClient.Get(email).Result()
	if err == redis.Nil {
		return false, nil
	}

	if err != nil {
		if err == redis.Nil {
			return false, nil
		}
		return false, err
	}

	emailCount, err := strconv.Atoi(emailCountStr)
	if err != nil {
		return false, err
	}

	if emailCount > 0 {
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
	form := "<h1>Contact</h1>\n"
	form = fmt.Sprintf("%s<form method=\"POST\">\n", form)
	form = fmt.Sprintf("%s<label>Voter Email: </label>\n", form)
	form = fmt.Sprintf("%s<input type=\"text\" name=\"email\" autocomplete=\"off\"><br/><br/>\n", form)
	form = fmt.Sprintf("%s<label>Candidates:</label><br/>\n", form)

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

	form = fmt.Sprintf("%s<input type=\"submit\" value=\"Vote\"><br/><br/>\n", form)
	form = fmt.Sprintf("%s<label>{{.Status}}</label>\n", form)
	form = fmt.Sprintf("%s</form>", form)

	message := []byte(form)
	err := ioutil.WriteFile("vote.html", message, 0644)
	return err
}

func doflags() {
	version := flag.Bool("v", false, "print current version and exit")

	flag.Parse()

	if *version {
		fmt.Println(VersionString)
		os.Exit(0)
	}
}

func main() {
	doflags()

	err := genHtmlForm()
	if err != nil {
		log.Fatal(err)
	}

	tmpl = template.Must(template.ParseFiles("vote.html"))

	redisClient = newRedisClient()
	router := mux.NewRouter()

	router.HandleFunc("/voteui", voteUIHandler)
	router.HandleFunc("/vote", getVotesHandler).Methods("GET")
	router.HandleFunc("/vote/{email}/{candidate}", setVoteHandler).Methods("POST")

	log.Fatal(http.ListenAndServe(":8000", router))
}
