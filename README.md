# vote
simple voting app

## Introduction
Docker compose will start the following servers:
1. Redis server for storing candidate : vote KV entries
1. GoLang Http vote server that provides voter registration, voting, and current candiate vote results
* /help (prints the available http endpoints)
* /register POST request body like: {"username":"richard@foo.com", "password":"dummy"} (returns a JWT Token)
* /voteui GET
  * An http form requires the following fields
     * jwt is the JWT Token associated with the voter who is casting his vote
     * candidate is one of the candidates to vote for
     * vote is a submit button - a user specified by email may only cast one vote
* /vote GET
  * A JSON response with the current tallied votes for each candidate will be returned
* /vote/{candidate} POST Header contains JWT token like "authorization: jwt xxx.yyy.zzz"
  * candidate is one of the following
    * "JoeBiden",
    * "BetoORourke", 
    * "BernieSanders",
    * "ElizabethWarren",
    * "KamalaHarris",
    * "DonaldTrump"

## Building
make build

## Running the servers
make run

## Example client requests
* curl -X GET http://localhost:8000/vote
  * {"BernieSanders":"1","BetoORourke":"0","DonaldTrump":"0","ElizabethWarren":"0","JoeBiden":"0","KamalaHarris":"0"}
* curl -X POST -d @cred.json http://localhost:8000/register
  * {"token":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJwYXNzd29yZCI6ImR1bW15IiwidXNlcm5hbWUiOiJyaWNoYXJkIn0.N4Z9B9GUOUGZvWJSf2qRg9bNBRNWZDiBwmjhTDpndLI"}
* curl -X POST --header "authorization: jwt eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJwYXNzd29yZCI6ImR1bW15IiwidXNlcm5hbWUiOiJyaWNoYXJkIn0.N4Z9B9GUOUGZvWJSf2qRg9bNBRNWZDiBwmjhTDpndLI" -v http://localhost:8000/vote/BernieSanders
* curl http://localhost:8000/help
  * POST /register Body like: {'username': 'xxx@aaa.com', 'password': 'yyy'} (returns a JWT token)
  * POST /vote/{candidate} Heder like: 'authorization: jwt xxx.yyy.zzz' (one vote per user!)
  * GET /vote (shows current votes per candidate)

## Limitations
* Hardening of JWT  shared secret
* Hardening of Redis credentials
* The candidates are hard coded in vote.go. Consider adding an endpoint to define a Ballot listing the candidates
* Therer are two voting http endpoints (CLI vs UI). Normalize
* Better logging https://github.com/sirupsen/logrus
