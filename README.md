# vote
simple voting app

## Introduction
Docker compose will start the following servers:
1. Redis server for storing candidate : vote KV entries
1. GoLang Http vote server that serves CLI based REST requests and Web Browser requests:
* /voteui GET
  * An http form requires the following fields
     * email is the user casting his vote
     * candidate is one of the candidates to vote for
     * vote is a submit button - a user specified by email may only cast one vote
* /vote GET
  * A JSON response with the current tallied votes will be returned
* /vote/{email}/{candidate} POST
  * email is the user casting his vote
  * candidate is one of the following
    * "JoeBiden",
    * "BetoORourke", 
    * "BernieSanders",
    * "ElizabethWarren",
    * "KamalaHarris",
    *"DonaldTrump"

## Building
make build

## Running the servers
make run

## Example client requests
* curl -X GET http://localhost:8000/vote
* curl -X POST http://localhost:8000/vote/richard@themauris.org/BernieSanders

## Limitations
* The ballot is hard coded in the main.go. Ideally, there would be an endpoint to define a ballot containing candidates
* The Redis and Vote server ports are hard coded. The Go code and docker-compose would have to sync on a configurable port
* It is easy to spoof a voter by plagiarizing the email address. Ideally, a voter registration endpoint that takes a few "credentials' and returns a voter Id would replace the email attribute.
* For large elections and better scalability there should be multiple vote servers behind a load balancer. This would require "service discovery" and could be implemented with Consul (as I am familiar with that!)
