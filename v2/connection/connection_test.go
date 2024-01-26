package connection_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/arobie1992/go-clarinet/v2/connection"
)

func TestParseWitnessSelector(t *testing.T) {
	tests := []struct {
		in              string
		expectedOutput connection.WitnessSelector
		expectedErr     error
	}{
		{"Sender", connection.WitnessSelectorSender(), nil},
		{"Receiver", connection.WitnessSelectorReceiver(), nil},
		{"Invalid", nil, errors.New("Unrecognized witness selector type: Invalid")},
	}

	for i, test := range tests {
		selector, err := connection.ParseWitnessSelector(test.in)
		if selector != test.expectedOutput {
			t.Errorf("Test %d incorrect output. Expected: %s, Got: %s", i, test.expectedOutput, selector)
			continue
		}
		if test.expectedErr == nil && err == nil {
			continue
		}
		if err == nil || test.expectedErr.Error() != err.Error() {
			t.Errorf("Incorrect error returned. Expected: %s, Got: %s", test.expectedErr, err)
		}
	}
}

func TestWitnessSelectorString(t *testing.T) {
	tests := []struct {
		ws connection.WitnessSelector
		expected string
	}{
		{connection.WitnessSelectorSender(), "Sender"},
		{connection.WitnessSelectorReceiver(), "Receiver"},
	}
	for i, test := range tests {
		if test.ws.String() != test.expected {
			t.Errorf("Test %d incorrect string output. Expected: %s, Got: %s", i, test.expected, test.ws.String())
		}
	}
}

func TestConnectRequestError(t *testing.T) {
	id, err := connection.NewRandomID()
	if err != nil {
		t.Fatalf("Failed to create connection ID: %s", err)
	}
	reasons := []string{"This is a test reason."}
	err = &connection.ConnectRejectError{id, reasons}
	expectedMsg := fmt.Sprintf("Connection %s connect request was rejected for the following reasons: %v", id, reasons)
	if err.Error() != expectedMsg {
		t.Fatalf("Incorrect error message. Expected: '%s', Got: '%s'", expectedMsg, err.Error())
	}
}

func TestWitnessRequestError(t *testing.T) {
	id, err := connection.NewRandomID()
	if err != nil {
		t.Fatalf("Failed to create connection ID: %s", err)
	}
	reasons := []string{"This is a test reason."}
	err = &connection.WitnessRejectError{id, reasons}
	expectedMsg := fmt.Sprintf("Connection %s witness request was rejected for the following reasons: %v", id, reasons)
	if err.Error() != expectedMsg {
		t.Fatalf("Incorrect error message. Expected: '%s', Got: '%s'", expectedMsg, err.Error())
	}
}
