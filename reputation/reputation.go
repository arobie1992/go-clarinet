package reputation

import (
	"math"

	"github.com/go-clarinet/repository"
	"github.com/libp2p/go-libp2p/core/peer"
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
	return r.value() > 50 && (r.value() > a.mean - a.stdev)
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
