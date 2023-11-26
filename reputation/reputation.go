package reputation

import (
	"errors"
	"math"
	"reflect"

	"github.com/arobie1992/go-clarinet/log"
	"github.com/arobie1992/go-clarinet/repository"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/mattn/go-sqlite3"
	"github.com/multiformats/go-multiaddr"
	"gorm.io/gorm"
)

type ReputationInfo struct {
	PeerId peer.ID `gorm:"primary_key"`
	Good   float64
	Total  float64
}

func (r ReputationInfo) value() float64 {
	return r.Good / r.Total
}

type AggregateReputations struct {
	reputations map[peer.ID]ReputationInfo
	mean        float64
	stdev       float64
}

func (a *AggregateReputations) IsTrusted(peerID peer.ID) bool {
	r, ok := a.reputations[peerID]
	if !ok {
		// we haven't seen them before and we default to assuming they're trustworthy
		return true
	}
	return r.value() > 0.5 && (r.value() >= a.mean-a.stdev)
}

func GetAll(peers peer.IDSlice) (AggregateReputations, error) {
	reputations := make([]ReputationInfo, len(peers))
	for i, p := range peers {
		reputations[i] = ReputationInfo{PeerId: p}
	}
	tx := repository.GetDB().Find(&reputations)
	if tx.Error != nil {
		return AggregateReputations{}, tx.Error
	}

	repMap := map[peer.ID]ReputationInfo{}
	for _, r := range reputations {
		repMap[r.PeerId] = r
	}

	mean := 0.0
	for _, r := range reputations {
		mean += r.value()
	}
	mean /= float64(len(reputations))

	variance := 0.0
	for _, r := range reputations {
		variance += math.Pow(r.value()-mean, 2)
	}
	variance /= float64(len(reputations) - 1)

	stdev := math.Sqrt(variance)

	return AggregateReputations{repMap, mean, stdev}, nil
}

func Reward(peerAddr string) {
	peerID, err := getPeerIDFromAddrString(peerAddr)
	if err != nil {
		log.Log().Errorf("Failed to convert peerAddr %s to peer ID so could not apply reward: %s", peerAddr, err)
		return
	}

	r := ReputationInfo{peerID, 0, 0}
	if err := ensureReputationInfoExists(&r); err != nil {
		log.Log().Errorf("Encountered error ensuring repInfo for %s existed so could not apply reward: %s", peerAddr, err)
	}

	// use update so the DB applies ACID and properly atomically updates the values
	updates := map[string]interface{}{
		"good":  gorm.Expr("good + ?", 1),
		"total": gorm.Expr("total + ?", 1),
	}
	repository.GetDB().Model(&r).Updates(updates)
}

func penalize(peerAddr string, penaltyStrength float64) {
	peerID, err := getPeerIDFromAddrString(peerAddr)
	if err != nil {
		log.Log().Errorf("Failed to convert peerAddr %s to peer ID so could not apply penalty: %s", peerAddr, err)
		return
	}

	r := ReputationInfo{peerID, 0, 0}
	if err := ensureReputationInfoExists(&r); err != nil {
		log.Log().Errorf("Encountered error ensuring repInfo for %s existed so could not apply penalty: %s", peerAddr, err)
	}

	// use update so the DB applies ACID and properly atomically updates the values
	repository.GetDB().Model(&r).Update("total", gorm.Expr("total + ?", penaltyStrength))
}

func WeakPenalize(peerAddr string) {
	penalize(peerAddr, 1)
}

func StrongPenalize(peerAddr string) {
	penalize(peerAddr, 3)
}

func getPeerIDFromAddrString(peerAddr string) (peer.ID, error) {
	maddr, err := multiaddr.NewMultiaddr(peerAddr)
	if err != nil {
		return "", errors.New("Failed to convert peerAddr to multiaddr.")
	}

	nodeID, err := maddr.ValueForProtocol(multiaddr.P_P2P)
	if err != nil {
		return "", errors.New("Failed to get node ID from maddr.")
	}

	return peer.Decode(nodeID)
}

func ensureReputationInfoExists(r *ReputationInfo) error {
	repository.GetDB().Find(r)
	if r.Total != 0 {
		// we found it in the DB so nothing to do
		return nil
	}

	tx := repository.GetDB().Create(r)
	if tx.Error == nil {
		// we successfully created it
		return nil
	}

	sqliteErr, ok := tx.Error.(sqlite3.Error)
	if !ok {
		// only sqlite3 is supported at the moment
		// this is a fatal because it should be noticed ASAP
		log.Log().Fatalf("Error is not sqlite3.Error: %s", reflect.TypeOf(tx.Error))
	}

	if sqliteErr.ExtendedCode == sqlite3.ErrConstraintPrimaryKey {
		// some other thread created it already so it's fine
		return nil
	}

	// some other error occurred so let the caller know
	return sqliteErr
}
