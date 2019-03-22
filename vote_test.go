package main

import (
	"testing"
)

const (
	InvalidToken = "Invalid token"
)

type TokenParseStruct struct {
	user     string
	password string
	err      string
}

func TestTokenParse(t *testing.T) {
	tables := []TokenParseStruct{
		{"user@foo.com", "dummy", ""},
		{"user@foo.com", "", InvalidToken},
		{"user", "dummy", InvalidToken},
		{"user@foo", "dummy", InvalidToken},
		{"@foo", "dummy", InvalidToken},
		{"", "dummy", InvalidToken},
	}

	for _, table := range tables {
		// log.Printf("\n\nHandling table %+v\n", table)
		tokenString, err := createJwtClaim(table.user, table.password)
		errorString := ""
		if err != nil {
			// log.Printf("createJwtClaim(\"%s\",\"%s\") returned error %v\n", table.user, table.password, err)
			errorString = InvalidToken
		}

		if errorString != table.err {
			t.Errorf("Unexpected error creating jwt claim for %+v: expected %s but got %s\n", table, table.err, errorString)
			continue
		} else {
			// we received an expected error so now need to continue with this test
			continue
		}

		tp := NewJwtTokenParser(tokenString)
		username, err := tp.TokenParse()
		// log.Printf("TokenParse(%s) username = %s err = %v\n", tokenString, username, err)
		if err != nil {
			errorString = InvalidToken
			if table.err != errorString {
				t.Errorf("Unexpected error response (got error when none expected) decoding jwt claim for %+v\n", table)
				continue
			}
		} else {
			if table.err == InvalidToken {
				t.Errorf("Unexpected error response (got no error when one expected) decoding jwt claim for %+v\n", table)
				continue
			}
		}
		if username != table.user {
			t.Errorf("Unexpected username parse result for %+v : expected %s but got %s\n", table, table.user, username)
			continue
		}
	}
}
