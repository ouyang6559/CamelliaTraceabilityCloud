package apierr

import (
	"errors"
	"net/http"
	"testing"
)

func TestFromMapping(t *testing.T) {
	if e := From(NotFound("farm")); e.Status != http.StatusNotFound || e.Code != CodeNotFound {
		t.Errorf("NotFound mapping wrong: %+v", e)
	}
	if e := From(errors.New("boom")); e.Status != http.StatusInternalServerError {
		t.Errorf("unknown error must map to 500, got %+v", e)
	}
	if e := From(nil); e != nil {
		t.Errorf("nil must stay nil, got %+v", e)
	}
	if e := From(Invalid("bad %s", "x")); e.Message != "bad x" {
		t.Errorf("format args lost: %q", e.Message)
	}
}
