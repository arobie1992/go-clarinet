package inmem_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/arobie1992/go-clarinet/starterimpl/inmem"
	"github.com/arobie1992/go-clarinet/v2/connection"
	"github.com/arobie1992/go-clarinet/v2/log"
	"github.com/arobie1992/go-clarinet/v2/peer"
	"github.com/arobie1992/go-clarinet/v2/reputation"
)

type noOpLog struct {
	level log.Level
}

func (l *noOpLog) Level() log.Level                   { return l.level }
func (l *noOpLog) Debug(fmtMsg string, values ...any) {}
func (l *noOpLog) Error(fmtMsg string, values ...any) {}
func (l *noOpLog) Info(fmtMsg string, values ...any)  {}
func (l *noOpLog) Trace(fmtMsg string, values ...any) {}
func (l *noOpLog) Warn(fmtMsg string, values ...any)  {}

type simpleId string

func (s simpleId) String() string {
	return string(s)
}

type testPeer struct {
	id        simpleId
	addresses []peer.Address
}

func (p *testPeer) Addresses() []peer.Address {
	return p.addresses
}

// ID implements peer.Peer.
func (p *testPeer) ID() peer.ID {
	return p.id
}

func verifyConnection(t *testing.T, cs connection.ConnectionStore, id connection.ID, sender, witness, receiver peer.ID, status connection.Status) {
	t.Helper()
	err := cs.Read(id, func(conn connection.Connection) error {
		if conn.ID() != id {
			t.Errorf("Connection ID does not match. Expected: %s, Got: %s", id, conn.ID())
		}
		if conn.Sender() != sender {
			t.Errorf("Sender does not match. Expected: %s, Got: %s", sender, conn.Sender())
		}
		if conn.Witness() != witness {
			t.Errorf("Witness does not match. Expected: %s, Got: %s", witness, conn.Witness())
		}
		if conn.Receiver() != receiver {
			t.Errorf("Receiver does not match. Expected: %s, Got: %s", receiver, conn.Receiver())
		}
		if conn.Status() != status {
			t.Errorf("Status does not match. Expected: %s, Got: %s", connection.Open(), conn.Status())
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Got error while reading connection: %s", err)
	}
}

func TestCreate(t *testing.T) {
	cs := inmem.NewConnectionStore(&noOpLog{log.Info()})
	sender := testPeer{"sender", []peer.Address{}}
	receiver := testPeer{"receiver", []peer.Address{}}
	status := connection.Open()
	connID, err := cs.Create(&sender, &receiver, status)
	if err != nil {
		t.Fatalf("Got error while creating connection: %s", err)
	}
	verifyConnection(t, cs, connID, sender.ID(), nil, receiver.ID(), status)
}

func TestAccept(t *testing.T) {
	cs := inmem.NewConnectionStore(&noOpLog{log.Info()})
	connID, err := connection.NewRandomID()
	if err != nil {
		t.Fatalf("Failed to create connection ID: %s", err)
	}
	sender := testPeer{"sender", []peer.Address{}}
	receiver := testPeer{"receiver", []peer.Address{}}
	status := connection.Open()
	if err := cs.Accept(connID, &sender, &receiver, status); err != nil {
		t.Fatalf("Failed to accept connection: %s", err)
	}
	verifyConnection(t, cs, connID, sender.ID(), nil, receiver.ID(), status)
}

func TestAcceptAlreadyExists(t *testing.T) {
	cs := inmem.NewConnectionStore(&noOpLog{log.Info()})
	connID, err := connection.NewRandomID()
	if err != nil {
		t.Fatalf("Failed to create connection ID: %s", err)
	}
	sender := testPeer{"sender", []peer.Address{}}
	receiver := testPeer{"receiver", []peer.Address{}}
	status := connection.Open()
	if err := cs.Accept(connID, &sender, &receiver, status); err != nil {
		t.Fatalf("Failed to accept connection: %s", err)
	}
	err = cs.Accept(connID, &sender, &receiver, status)
	if err == nil {
		t.Fatalf("Accept should not have succeeded because connection ID was already present.")
	}
	expectedErrMsg := fmt.Sprintf("A connection already exists for ID: %s", connID)
	if err.Error() != expectedErrMsg {
		t.Fatalf("Message did not match. Expected: '%s', Got: '%s'", expectedErrMsg, err.Error())
	}
}

func TestAll(t *testing.T) {
	cs := inmem.NewConnectionStore(&noOpLog{log.Info()})
	sender := testPeer{"sender", []peer.Address{}}
	receiver := testPeer{"receiver", []peer.Address{}}
	status := connection.Open()
	expectedIDs := []connection.ID{}
	for i := 0; i < 3; i += 1 {
		id, err := cs.Create(&sender, &receiver, status)
		if err != nil {
			t.Fatalf("Encountered error creating connection number %d: %s", i, err)
		}
		expectedIDs = append(expectedIDs, id)
	}
	ids, err := cs.All()
	if err != nil {
		t.Fatalf("Encountered error in calling cs.All(): %s", err)
	}
	if len(ids) != len(expectedIDs) {
		t.Fatalf("Incorrect number of IDs created. Expected: %d, Got: %d", len(expectedIDs), len(ids))
	}
	for _, expectedID := range expectedIDs {
		found := false
		for _, id := range ids {
			if expectedID == id {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("ID %s not in list returned from cs.All()", expectedID)
		}
	}
	for _, id := range ids {
		found := false
		for _, expectedID := range expectedIDs {
			if expectedID == id {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Unexpected ID %s returned from cs.All()", id)
		}
	}
}

func TestUpdate(t *testing.T) {
	cs := inmem.NewConnectionStore(&noOpLog{log.Info()})
	sender := testPeer{"sender", []peer.Address{}}
	receiver := testPeer{"receiver", []peer.Address{}}
	status := connection.Open()
	connID, err := cs.Create(&sender, &receiver, status)
	if err != nil {
		t.Fatalf("Got error while creating connection: %s", err)
	}
	verifyConnection(t, cs, connID, sender.ID(), nil, receiver.ID(), status)

	witness := testPeer{"witness", []peer.Address{}}
	err = cs.Update(connID, func(conn connection.Connection) (connection.Connection, error) {
		return conn.SetWitness(witness.ID())
	})
	if err != nil {
		t.Fatalf("Error occurred during update call: %s", err)
	}
	verifyConnection(t, cs, connID, sender.ID(), witness.ID(), receiver.ID(), status)
}

func TestConnectionUpdateRollback(t *testing.T) {
	cs := inmem.NewConnectionStore(&noOpLog{log.Info()})
	sender := testPeer{"sender", []peer.Address{}}
	receiver := testPeer{"receiver", []peer.Address{}}
	status := connection.Open()
	connID, err := cs.Create(&sender, &receiver, status)
	if err != nil {
		t.Fatalf("Got error while creating connection: %s", err)
	}
	verifyConnection(t, cs, connID, sender.ID(), nil, receiver.ID(), status)

	witness := testPeer{"witness", []peer.Address{}}
	err = cs.Update(connID, func(conn connection.Connection) (connection.Connection, error) {
		conn.SetWitness(witness.ID())
		return nil, nil
	})
	if err != nil {
		t.Fatalf("Error occurred during update call: %s", err)
	}
	verifyConnection(t, cs, connID, sender.ID(), nil, receiver.ID(), status)
}

func TestConnectionUpdateErrorSignaling(t *testing.T) {
	cs := inmem.NewConnectionStore(&noOpLog{log.Info()})
	sender := testPeer{"sender", []peer.Address{}}
	receiver := testPeer{"receiver", []peer.Address{}}
	status := connection.Open()
	connID, err := cs.Create(&sender, &receiver, status)
	if err != nil {
		t.Fatalf("Got error while creating connection: %s", err)
	}
	verifyConnection(t, cs, connID, sender.ID(), nil, receiver.ID(), status)

	witness := testPeer{"witness", []peer.Address{}}
	err = cs.Update(connID, func(conn connection.Connection) (connection.Connection, error) {
		conn, _ = conn.SetWitness(witness.ID())
		return conn, errors.New("TestErr")
	})
	if err == nil || err.Error() != "TestErr" {
		t.Fatalf("Error from update not correctly returned. Returned error was %s", err)
	}
	verifyConnection(t, cs, connID, sender.ID(), witness.ID(), receiver.ID(), status)
}

func TestConnectionUpdateOneAtATime(t *testing.T) {
	cs := inmem.NewConnectionStore(&noOpLog{log.Info()})
	sender := testPeer{"sender", []peer.Address{}}
	receiver := testPeer{"receiver", []peer.Address{}}
	status := connection.Open()
	connID, err := cs.Create(&sender, &receiver, status)
	if err != nil {
		t.Fatalf("Got error while creating connection: %s", err)
	}
	verifyConnection(t, cs, connID, sender.ID(), nil, receiver.ID(), status)

	witness1 := testPeer{"witness1", []peer.Address{}}
	witness2 := testPeer{"witness2", []peer.Address{}}
	testWG := sync.WaitGroup{}
	testWG.Add(2)
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		err := cs.Update(connID, func(conn connection.Connection) (connection.Connection, error) {
			wg.Done()
			// wait long enough that the other go func should have ideally had time to start and hit the write lock
			// sleep after we notify the wait group because
			time.Sleep(2 * time.Second)
			return conn.SetWitness(witness1.ID())
		})
		if err != nil {
			t.Errorf("Error occurred during update call: %s", err)
		}
		testWG.Done()
	}()
	if t.Failed() {
		t.FailNow()
	}
	go func() {
		// make sure the first func gets the write permission first
		wg.Wait()
		err := cs.Update(connID, func(conn connection.Connection) (connection.Connection, error) {
			if conn.Witness() != witness1.ID() {
				t.Errorf("Second update occurred before first update completed.")
			}
			return conn.SetWitness(witness2.ID())
		})
		if err != nil {
			t.Errorf("Error occurred during update call: %s", err)
		}
		testWG.Done()
	}()
	// wait for both threads to finish up so we can know if the test succeeded.
	testWG.Wait()
}

func TestConnectionReadDoesNotAllowAlteration(t *testing.T) {
	cs := inmem.NewConnectionStore(&noOpLog{log.Info()})
	sender := testPeer{"sender", []peer.Address{}}
	receiver := testPeer{"receiver", []peer.Address{}}
	status := connection.Open()
	connID, err := cs.Create(&sender, &receiver, status)
	if err != nil {
		t.Fatalf("Got error while creating connection: %s", err)
	}
	verifyConnection(t, cs, connID, sender.ID(), nil, receiver.ID(), status)

	witness := testPeer{"witness", []peer.Address{}}
	err = cs.Read(connID, func(conn connection.Connection) error {
		conn, _ = conn.SetWitness(witness.ID())
		return nil
	})
	if err != nil {
		t.Fatalf("Encountered error during read call: %s", err)
	}
	verifyConnection(t, cs, connID, sender.ID(), nil, receiver.ID(), status)
}

func TestConnectionReadErrorSignaling(t *testing.T) {
	cs := inmem.NewConnectionStore(&noOpLog{log.Info()})
	sender := testPeer{"sender", []peer.Address{}}
	receiver := testPeer{"receiver", []peer.Address{}}
	status := connection.Open()
	connID, err := cs.Create(&sender, &receiver, status)
	if err != nil {
		t.Fatalf("Got error while creating connection: %s", err)
	}
	verifyConnection(t, cs, connID, sender.ID(), nil, receiver.ID(), status)

	err = cs.Read(connID, func(conn connection.Connection) error {
		return errors.New("Read test error")
	})
	if err == nil || err.Error() != "Read test error" {
		t.Fatalf("Error from read not correctly returned. Returned error was %s", err)
	}
}

func TestConnectionReadAllowsMultiple(t *testing.T) {
	cs := inmem.NewConnectionStore(&noOpLog{log.Info()})
	sender := testPeer{"sender", []peer.Address{}}
	receiver := testPeer{"receiver", []peer.Address{}}
	status := connection.Open()
	connID, err := cs.Create(&sender, &receiver, status)
	if err != nil {
		t.Fatalf("Got error while creating connection: %s", err)
	}
	verifyConnection(t, cs, connID, sender.ID(), nil, receiver.ID(), status)

	testWG := sync.WaitGroup{}
	testWG.Add(2)
	start := time.Now()
	go func() {
		err := cs.Read(connID, func(conn connection.Connection) error {
			time.Sleep(2 * time.Second)
			return nil
		})
		if err != nil {
			t.Errorf("Error occurred during read call: %s", err)
		}
		testWG.Done()
	}()
	go func() {
		err := cs.Read(connID, func(conn connection.Connection) error {
			time.Sleep(2 * time.Second)
			return nil
		})
		if err != nil {
			t.Errorf("Error occurred during read call: %s", err)
		}
		testWG.Done()
	}()
	testWG.Wait()
	end := time.Now()
	elapsed := end.Sub(start)
	if !(elapsed < 4*time.Second) {
		t.Errorf("Both reads took %s to complete meaning they were likely serial.", elapsed)
	}
}

func TestConnectionUpdateBlocksRead(t *testing.T) {
	cs := inmem.NewConnectionStore(&noOpLog{log.Info()})
	sender := testPeer{"sender", []peer.Address{}}
	receiver := testPeer{"receiver", []peer.Address{}}
	status := connection.Open()
	connID, err := cs.Create(&sender, &receiver, status)
	if err != nil {
		t.Fatalf("Got error while creating connection: %s", err)
	}
	verifyConnection(t, cs, connID, sender.ID(), nil, receiver.ID(), status)

	witness := testPeer{"witness1", []peer.Address{}}
	testWG := sync.WaitGroup{}
	testWG.Add(2)
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		err := cs.Update(connID, func(conn connection.Connection) (connection.Connection, error) {
			wg.Done()
			// wait long enough that the other go func should have ideally had time to start and hit the write lock
			// sleep after we notify the wait group because
			time.Sleep(2 * time.Second)
			return conn.SetWitness(witness.ID())
		})
		if err != nil {
			t.Errorf("Error occurred during update call: %s", err)
		}
		testWG.Done()
	}()
	if t.Failed() {
		t.FailNow()
	}
	go func() {
		// make sure the first func gets to run first
		wg.Wait()
		err := cs.Read(connID, func(conn connection.Connection) error {
			if conn.Witness() != witness.ID() {
				t.Errorf("Read occurred before update completed.")
			}
			return nil
		})
		if err != nil {
			t.Errorf("Error occurred during update call: %s", err)
		}
		testWG.Done()
	}()
	// wait for both threads to finish up so we can know if the test succeeded.
	testWG.Wait()
}

func TestConnectionReadBlocksUpdate(t *testing.T) {
	cs := inmem.NewConnectionStore(&noOpLog{log.Info()})
	sender := testPeer{"sender", []peer.Address{}}
	receiver := testPeer{"receiver", []peer.Address{}}
	status := connection.Open()
	connID, err := cs.Create(&sender, &receiver, status)
	if err != nil {
		t.Fatalf("Got error while creating connection: %s", err)
	}
	verifyConnection(t, cs, connID, sender.ID(), nil, receiver.ID(), status)

	witness := testPeer{"witness2", []peer.Address{}}
	testWG := sync.WaitGroup{}
	testWG.Add(2)
	wg := sync.WaitGroup{}
	wg.Add(1)
	start := time.Now()
	go func() {
		err := cs.Read(connID, func(conn connection.Connection) error {
			wg.Done()
			// wait long enough that the other go func should have ideally had time to start and hit the write lock
			// sleep after we notify the wait group because
			time.Sleep(2 * time.Second)
			return nil
		})
		if err != nil {
			t.Errorf("Error occurred during read call: %s", err)
		}
		testWG.Done()
	}()
	if t.Failed() {
		t.FailNow()
	}
	go func() {
		// make sure the first func gets to run first
		wg.Wait()
		err := cs.Update(connID, func(conn connection.Connection) (connection.Connection, error) {
			end := time.Now()
			elapsed := end.Sub(start)
			if elapsed < 2*time.Second {
				t.Errorf("Elapsed was %s which is less than the time the read op slept for.", elapsed)
			}
			return conn.SetWitness(witness.ID())
		})
		if err != nil {
			t.Errorf("Error occurred during update call: %s", err)
		}
		testWG.Done()
	}()
	// wait for both threads to finish up so we can know if the test succeeded.
	testWG.Wait()
}

func TestReputationOperations(t *testing.T) {
	rs := inmem.NewReputationStore(nil)
	peerID := simpleId("peerID")
	tests := []struct {
		updateFunc  func(rep reputation.Reputation) (reputation.Reputation, error)
		expectedRep float64
	}{
		{func(rep reputation.Reputation) (reputation.Reputation, error) { return nil, nil }, float64(1)},
		{func(rep reputation.Reputation) (reputation.Reputation, error) { return rep.Reward(), nil }, float64(1)},
		{func(rep reputation.Reputation) (reputation.Reputation, error) { return rep.WeakPenalize(), nil }, float64(1) / float64(2)},
		{func(rep reputation.Reputation) (reputation.Reputation, error) { return rep.StrongPenalize(), nil }, float64(1) / float64(5)},
		{func(rep reputation.Reputation) (reputation.Reputation, error) { return rep.Reward(), nil }, float64(2) / float64(6)},
	}
	for i, test := range tests {
		if err := rs.Update(peerID, test.updateFunc); err != nil {
			t.Fatalf("Test numer %d encountered error while running updateFunc: %s", i, err)
		}
		err := rs.Read(peerID, func(rep reputation.Reputation) error {
			if rep.Value() != test.expectedRep {
				t.Errorf("Test number %d expected value did not match. Expected: %f, Got: %f", i, test.expectedRep, rep.Value())
			}
			return nil
		})
		if err != nil {
			t.Fatalf("Test number %d encountered error while checking expected value: %s", i, err)
		}
	}
}

type testRep struct {
	peerID peer.ID
}

func (t testRep) PeerID() peer.ID {
	return t.peerID
}

func (r testRep) Reward() reputation.Reputation {
	return r
}

func (r testRep) StrongPenalize() reputation.Reputation {
	return r
}

func (r testRep) Value() float64 {
	return 0.678
}

func (r testRep) WeakPenalize() reputation.Reputation {
	return r
}

func TestNewReputationStoreCustomRepFunc(t *testing.T) {
	called := false
	rs := inmem.NewReputationStore(func(peerID peer.ID) reputation.Reputation {
		called = true
		return testRep{peerID}
	})
	err := rs.Read(simpleId("peerID"), func(rep reputation.Reputation) error {
		if rep.Value() != 0.678 {
			t.Errorf("Reputation did not match expected. Expected: %f, Got: %f", 0.678, rep.Value())
		}
		return nil
	})
	if t.Failed() {
		t.FailNow()
	}
	if err != nil {
		t.Fatalf("Error while reading reputation: %s", err)
	}
	if !called {
		t.Fatalf("Provided reputation function was not called.")
	}
}

func TestReputationUpdateRollback(t *testing.T) {
	rs := inmem.NewReputationStore(nil)
	peerID := simpleId("peerID")
	err := rs.Update(peerID, func(rep reputation.Reputation) (reputation.Reputation, error) {
		if rep.Value() != 1 {
			t.Errorf("Initial reputation value did not match expected. Expected 1, Got: %f", rep.Value())
			return nil, nil
		}
		rep = rep.WeakPenalize()
		if rep.Value() != 0 {
			t.Error("WeakPenalize opration did not succeed.")
		}
		return nil, nil
	})
	if t.Failed() {
		t.FailNow()
	}
	if err != nil {
		t.Fatalf("Error during update: %s", err)
	}

	rs.Read(peerID, func(rep reputation.Reputation) error {
		if rep.Value() != 1 {
			t.Errorf("Update operation erroneously persisted.")
		}
		return nil
	})
}

func TestReputationUpdateErrorSignaling(t *testing.T) {
	rs := inmem.NewReputationStore(nil)
	peerID := simpleId("peerID")
	err := rs.Update(peerID, func(rep reputation.Reputation) (reputation.Reputation, error) {
		return rep.WeakPenalize(), errors.New("Rep update err")
	})
	if err == nil || err.Error() != "Rep update err" {
		t.Fatalf("Error from update not correctly returned. Returned error was %s", err)
	}
	rs.Read(peerID, func(rep reputation.Reputation) error {
		if rep.Value() != 0 {
			t.Errorf("Update operation was not persisted.")
		}
		return nil
	})
}

func TestReputationUpdateOneAtATime(t *testing.T) {
	rs := inmem.NewReputationStore(nil)
	peerID := simpleId("peerID")
	testWG := sync.WaitGroup{}
	testWG.Add(2)
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		err := rs.Update(peerID, func(rep reputation.Reputation) (reputation.Reputation, error) {
			wg.Done()
			time.Sleep(2 * time.Second)
			return rep.WeakPenalize(), nil
		})
		if err != nil {
			t.Errorf("Encountered error during update: %s", err)
		}
		testWG.Done()
	}()
	if t.Failed() {
		t.FailNow()
	}

	go func() {
		wg.Wait()
		err := rs.Update(peerID, func(rep reputation.Reputation) (reputation.Reputation, error) {
			if rep.Value() != 0 {
				t.Errorf("Second update failed to wait for first to finish: %f", rep.Value())
			}
			return rep.Reward(), nil
		})
		if err != nil {
			t.Errorf("Encountered error during update: %s", err)
		}
		testWG.Done()
	}()
	// wait for goroutines to finish
	testWG.Wait()
}

func TestReputationReadDoesNotAllowAlteration(t *testing.T) {
	rs := inmem.NewReputationStore(nil)
	peerID := simpleId("peerID")
	err := rs.Read(peerID, func(rep reputation.Reputation) error {
		rep = rep.WeakPenalize()
		if rep.Value() != 0 {
			t.Errorf("WeakPenalize operation failed.")
		}
		return nil
	})
	if err != nil {
		t.Errorf("Encountered error during update: %s", err)
	}

	rs.Read(peerID, func(rep reputation.Reputation) error {
		if rep.Value() != 1 {
			t.Errorf("Previous read operation persisted the change.")
		}
		return nil
	})
}

func TestReputationReadErrorSignaling(t *testing.T) {
	rs := inmem.NewReputationStore(nil)
	peerID := simpleId("peerID")
	err := rs.Read(peerID, func(rep reputation.Reputation) error {
		return errors.New("Reputation read error")
	})
	if err == nil || err.Error() != "Reputation read error" {
		t.Fatalf("Error from read not correctly returned. Returned error was %s", err)
	}
}

func TestReputationReadAllowsMultiple(t *testing.T) {
	rs := inmem.NewReputationStore(nil)
	peerID := simpleId("peerID")

	testWG := sync.WaitGroup{}
	testWG.Add(2)
	start := time.Now()
	go func() {
		err := rs.Read(peerID, func(rep reputation.Reputation) error {
			time.Sleep(2 * time.Second)
			return nil
		})
		if err != nil {
			t.Errorf("Error occurred during read call: %s", err)
		}
		testWG.Done()
	}()
	go func() {
		err := rs.Read(peerID, func(rep reputation.Reputation) error {
			time.Sleep(2 * time.Second)
			return nil
		})
		if err != nil {
			t.Errorf("Error occurred during read call: %s", err)
		}
		testWG.Done()
	}()
	testWG.Wait()
	end := time.Now()
	elapsed := end.Sub(start)
	if !(elapsed < 4*time.Second) {
		t.Errorf("Both reads took %s to complete meaning they were likely serial.", elapsed)
	}
}

func TestReputationUpdateBlocksRead(t *testing.T) {
	rs := inmem.NewReputationStore(nil)
	peerID := simpleId("peerID")

	testWG := sync.WaitGroup{}
	testWG.Add(2)
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		err := rs.Update(peerID, func(rep reputation.Reputation) (reputation.Reputation, error) {
			wg.Done()
			// wait long enough that the other go func should have ideally had time to start and hit the write lock
			// sleep after we notify the wait group because
			time.Sleep(2 * time.Second)
			return rep.WeakPenalize(), nil
		})
		if err != nil {
			t.Errorf("Error occurred during update call: %s", err)
		}
		testWG.Done()
	}()
	if t.Failed() {
		t.FailNow()
	}

	go func() {
		// make sure the first func gets to run first
		wg.Wait()
		err := rs.Read(peerID, func(rep reputation.Reputation) error {
			if rep.Value() != 0 {
				t.Errorf("Read occurred before update completed: %f", rep.Value())
			}
			return nil
		})
		if err != nil {
			t.Errorf("Error occurred during update call: %s", err)
		}
		testWG.Done()
	}()
	// wait for both threads to finish up so we can know if the test succeeded.
	testWG.Wait()
}

func TestReputationReadBlocksUpdate(t *testing.T) {
	rs := inmem.NewReputationStore(nil)
	peerID := simpleId("peerID")

	testWG := sync.WaitGroup{}
	testWG.Add(2)
	wg := sync.WaitGroup{}
	wg.Add(1)
	start := time.Now()
	go func() {
		err := rs.Read(peerID, func(rep reputation.Reputation) error {
			wg.Done()
			// wait long enough that the other go func should have ideally had time to start and hit the write lock
			// sleep after we notify the wait group because
			time.Sleep(2 * time.Second)
			return nil
		})
		if err != nil {
			t.Errorf("Error occurred during read call: %s", err)
		}
		testWG.Done()
	}()
	if t.Failed() {
		t.FailNow()
	}

	go func() {
		// make sure the first func gets to run first
		wg.Wait()
		err := rs.Update(peerID, func(rep reputation.Reputation) (reputation.Reputation, error) {
			end := time.Now()
			elapsed := end.Sub(start)
			if elapsed < 2*time.Second {
				t.Errorf("Elapsed was %s which is less than the time the read op slept for.", elapsed)
			}
			return rep.WeakPenalize(), nil
		})
		if err != nil {
			t.Errorf("Error occurred during update call: %s", err)
		}
		testWG.Done()
	}()
	// wait for both threads to finish up so we can know if the test succeeded.
	testWG.Wait()
}

// TODO Add message tests once message piecces have been more fully implemented
