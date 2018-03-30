/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package discovery

import (
	"bytes"
	"context"
	"math/rand"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/hyperledger/fabric/gossip/util"
	"github.com/hyperledger/fabric/protos/discovery"
	"github.com/hyperledger/fabric/protos/msp"
	"github.com/pkg/errors"
)

var (
	configTypes = []discovery.QueryType{discovery.ConfigQueryType, discovery.PeerMembershipQueryType, discovery.ChaincodeQueryType}
)

type client struct {
	lastRequest      []byte
	lastSignature    []byte
	createConnection Dialer
	authInfo         *discovery.AuthInfo
	signRequest      Signer
}

// NewRequest creates a new request
func NewRequest() *Request {
	r := &Request{
		queryMapping: make(map[discovery.QueryType]map[string]int),
		Request:      &discovery.Request{},
	}
	// pre-populate types
	for _, queryType := range configTypes {
		r.queryMapping[queryType] = make(map[string]int)
	}
	return r
}

// Request aggregates several queries inside it
type Request struct {
	lastChannel string
	lastIndex   int
	// map from query type to channel (or channel + chaincode) to expected index in response
	queryMapping map[discovery.QueryType]map[string]int
	*discovery.Request
}

// AddConfigQuery adds to the request a config query
func (req *Request) AddConfigQuery() *Request {
	ch := req.lastChannel
	q := &discovery.Query_ConfigQuery{
		ConfigQuery: &discovery.ConfigQuery{},
	}
	req.Queries = append(req.Queries, &discovery.Query{
		Channel: ch,
		Query:   q,
	})
	req.addQueryMapping(discovery.ConfigQueryType, ch)
	return req
}

// AddEndorsersQuery adds to the request a query for given chaincodes
func (req *Request) AddEndorsersQuery(chaincodes ...string) *Request {
	ch := req.lastChannel
	q := &discovery.Query_CcQuery{
		CcQuery: &discovery.ChaincodeQuery{
			Chaincodes: chaincodes,
		},
	}
	req.Queries = append(req.Queries, &discovery.Query{
		Channel: ch,
		Query:   q,
	})
	req.addQueryMapping(discovery.ChaincodeQueryType, ch)
	return req
}

// AddPeersQuery adds to the request a peer query
func (req *Request) AddPeersQuery() *Request {
	ch := req.lastChannel
	q := &discovery.Query_PeerQuery{
		PeerQuery: &discovery.PeerMembershipQuery{},
	}
	req.Queries = append(req.Queries, &discovery.Query{
		Channel: ch,
		Query:   q,
	})
	req.addQueryMapping(discovery.PeerMembershipQueryType, ch)
	return req
}

// OfChannel sets the next queries added to be in the given channel's context
func (req *Request) OfChannel(ch string) *Request {
	req.lastChannel = ch
	return req
}

func (req *Request) addQueryMapping(queryType discovery.QueryType, key string) {
	req.queryMapping[queryType][key] = req.lastIndex
	req.lastIndex++
}

// Send sends the request and returns the response, or error on failure
func (c *client) Send(ctx context.Context, req *Request) (Response, error) {
	req.Authentication = c.authInfo
	payload, err := proto.Marshal(req.Request)
	if err != nil {
		return nil, errors.Wrap(err, "failed marshaling Request to bytes")
	}

	sig := c.lastSignature
	// Only sign the Request if it is different than the previous Request sent.
	// Otherwise, use the last signature from the previous send.
	// This is not only to save CPU cycles in the client-side,
	// but also for the server side to be able to memoize the signature verification.
	// We have the use the previous signature, because many signature schemes are not deterministic.
	if !bytes.Equal(c.lastRequest, payload) {
		sig, err = c.signRequest(payload)
		if err != nil {
			return nil, errors.Wrap(err, "failed signing Request")
		}
	}

	// Remember this Request and the corresponding signature, in order to skip signing next time
	// and reuse the signature
	c.lastRequest = payload
	c.lastSignature = sig

	conn, err := c.createConnection()
	if err != nil {
		return nil, errors.Wrap(err, "failed connecting to discovery service")
	}

	defer conn.Close()
	defer func() {
		req.Queries = nil
	}()

	cl := discovery.NewDiscoveryClient(conn)
	resp, err := cl.Discover(ctx, &discovery.SignedRequest{
		Payload:   payload,
		Signature: sig,
	})
	if err != nil {
		return nil, errors.Wrap(err, "discovery service refused our Request")
	}
	if n := len(resp.Results); n != req.lastIndex {
		return nil, errors.Errorf("Sent %d queries but received %d responses back", req.lastIndex, n)
	}
	return computeResponse(req.queryMapping, resp)
}

type resultOrError interface {
}

type response map[key]resultOrError

type channelResponse struct {
	response
	channel string
}

func (cr *channelResponse) Config() (*discovery.ConfigResult, error) {
	res, exists := cr.response[key{
		queryType: discovery.ConfigQueryType,
		channel:   cr.channel,
	}]

	if !exists {
		return nil, ErrNotFound
	}

	if config, isConfig := res.(*discovery.ConfigResult); isConfig {
		return config, nil
	}

	return nil, res.(error)
}

func (cr *channelResponse) Peers() ([]*Peer, error) {
	res, exists := cr.response[key{
		queryType: discovery.PeerMembershipQueryType,
		channel:   cr.channel,
	}]

	if !exists {
		return nil, ErrNotFound
	}

	if peers, isPeers := res.([]*Peer); isPeers {
		return peers, nil
	}

	return nil, res.(error)
}

func (cr *channelResponse) Endorsers(cc string) (Endorsers, error) {
	// If we have a key that has no chaincode field,
	// it means it's an error returned from the service
	if err, exists := cr.response[key{
		queryType: discovery.ChaincodeQueryType,
		channel:   cr.channel,
	}]; exists {
		return nil, err.(error)
	}

	// Else, the service returned a response that isn't an error
	res, exists := cr.response[key{
		queryType: discovery.ChaincodeQueryType,
		channel:   cr.channel,
		chaincode: cc,
	}]

	if !exists {
		return nil, ErrNotFound
	}

	desc := res.(*endorsementDescriptor)
	rand.Seed(time.Now().Unix())
	randomLayoutIndex := rand.Intn(len(desc.layouts))
	layout := desc.layouts[randomLayoutIndex]
	var endorsers []*Peer
	for grp, count := range layout {
		endorsersOfGrp := randomEndorsers(count, desc.endorsersByGroups[grp])
		if len(endorsersOfGrp) < count {
			return nil, errors.Errorf("layout has a group that requires at least %d peers, but only %d peers are known", count, len(endorsersOfGrp))
		}
		endorsers = append(endorsers, endorsersOfGrp...)
	}

	return endorsers, nil
}

func (resp response) ForChannel(ch string) ChannelResponse {
	return &channelResponse{
		channel:  ch,
		response: resp,
	}
}

type key struct {
	queryType discovery.QueryType
	channel   string
	chaincode string
}

func computeResponse(queryMapping map[discovery.QueryType]map[string]int, r *discovery.Response) (response, error) {
	var err error
	resp := make(response)
	for configType, channel2index := range queryMapping {
		switch configType {
		case discovery.ConfigQueryType:
			err = resp.mapConfig(channel2index, r)
		case discovery.ChaincodeQueryType:
			err = resp.mapEndorsers(channel2index, r)
		case discovery.PeerMembershipQueryType:
			err = resp.mapPeerMembership(channel2index, r)
		}
		if err != nil {
			return nil, err
		}
	}

	return resp, err
}

func (resp response) mapConfig(channel2index map[string]int, r *discovery.Response) error {
	for ch, index := range channel2index {
		config, err := r.ConfigAt(index)
		if config == nil && err == nil {
			return errors.Errorf("expected QueryResult of either ConfigResult or Error but got %v instead", r.Results[index])
		}
		key := key{
			queryType: discovery.ConfigQueryType,
			channel:   ch,
		}

		if err != nil {
			resp[key] = errors.New(err.Content)
			continue
		}

		resp[key] = config
	}
	return nil
}

func (resp response) mapPeerMembership(channel2index map[string]int, r *discovery.Response) error {
	for ch, index := range channel2index {
		membersRes, err := r.MembershipAt(index)
		if membersRes == nil && err == nil {
			return errors.Errorf("expected QueryResult of either PeerMembershipResult or Error but got %v instead", r.Results[index])
		}
		key := key{
			queryType: discovery.PeerMembershipQueryType,
			channel:   ch,
		}

		if err != nil {
			resp[key] = errors.New(err.Content)
			continue
		}

		peers, err2 := peersForChannel(membersRes)
		if err2 != nil {
			return errors.Wrap(err2, "failed constructing peer membership out of response")
		}

		resp[key] = peers
	}
	return nil
}

func peersForChannel(membersRes *discovery.PeerMembershipResult) ([]*Peer, error) {
	var peers []*Peer
	for org, peersOfCurrentOrg := range membersRes.PeersByOrg {
		for _, peer := range peersOfCurrentOrg.Peers {
			aliveMsg, err := peer.MembershipInfo.ToGossipMessage()
			if err != nil {
				return nil, errors.Wrap(err, "failed unmarshaling alive message")
			}
			stateInfoMsg, err := peer.StateInfo.ToGossipMessage()
			if err != nil {
				return nil, errors.Wrap(err, "failed unmarshaling stateInfo message")
			}
			peers = append(peers, &Peer{
				MSPID:            org,
				Identity:         peer.Identity,
				AliveMessage:     aliveMsg,
				StateInfoMessage: stateInfoMsg,
			})
		}
	}
	return peers, nil
}

func (resp response) mapEndorsers(channel2index map[string]int, r *discovery.Response) error {
	for ch, index := range channel2index {
		ccQueryRes, err := r.EndorsersAt(index)
		if ccQueryRes == nil && err == nil {
			return errors.Errorf("expected QueryResult of either ChaincodeQueryResult or Error but got %v instead", r.Results[index])
		}

		key := key{
			queryType: discovery.ChaincodeQueryType,
			channel:   ch,
		}

		if err != nil {
			resp[key] = errors.New(err.Content)
			continue
		}

		if err := resp.mapEndorsersOfChannel(ccQueryRes, ch); err != nil {
			return errors.Wrapf(err, "failed assembling endorsers of channel %s", ch)
		}
	}
	return nil
}

func (resp response) mapEndorsersOfChannel(ccRs *discovery.ChaincodeQueryResult, channel string) error {
	for _, desc := range ccRs.Content {
		key := key{
			queryType: discovery.ChaincodeQueryType,
			channel:   channel,
			chaincode: desc.Chaincode,
		}

		descriptor, err := resp.createEndorsementDescriptor(desc, channel)
		if err != nil {
			return err
		}
		resp[key] = descriptor
	}

	return nil
}

func (resp response) createEndorsementDescriptor(desc *discovery.EndorsementDescriptor, channel string) (*endorsementDescriptor, error) {
	descriptor := &endorsementDescriptor{
		layouts:           []map[string]int{},
		endorsersByGroups: make(map[string][]*Peer),
	}
	for _, l := range desc.Layouts {
		currentLayout := make(map[string]int)
		descriptor.layouts = append(descriptor.layouts, currentLayout)
		for grp, count := range l.QuantitiesByGroup {
			if _, exists := desc.EndorsersByGroups[grp]; !exists {
				return nil, errors.Errorf("group %s isn't mapped to endorsers, but exists in a layout", grp)
			}
			currentLayout[grp] = int(count)
		}
	}

	for grp, peers := range desc.EndorsersByGroups {
		var endorsers []*Peer
		for _, p := range peers.Peers {
			peer, err := endorser(p, desc.Chaincode, channel)
			if err != nil {
				return nil, errors.Wrap(err, "failed creating endorser object")
			}
			endorsers = append(endorsers, peer)
		}
		descriptor.endorsersByGroups[grp] = endorsers
	}

	return descriptor, nil
}

func endorser(peer *discovery.Peer, chaincode, channel string) (*Peer, error) {
	if peer.MembershipInfo == nil || peer.StateInfo == nil {
		return nil, errors.Errorf("received empty envelope(s) for endorsers for chaincode %s, channel %s", chaincode, channel)
	}
	aliveMsg, err := peer.MembershipInfo.ToGossipMessage()
	if err != nil {
		return nil, errors.Wrap(err, "failed unmarshaling gossip envelope to alive message")
	}
	stateInfMsg, err := peer.StateInfo.ToGossipMessage()
	if err != nil {
		return nil, errors.Wrap(err, "failed unmarshaling gossip envelope to state info message")
	}
	sId := &msp.SerializedIdentity{}
	if err := proto.Unmarshal(peer.Identity, sId); err != nil {
		return nil, errors.Wrap(err, "failed unmarshaling peer's identity")
	}
	return &Peer{
		Identity:         peer.Identity,
		StateInfoMessage: stateInfMsg,
		AliveMessage:     aliveMsg,
		MSPID:            sId.Mspid,
	}, nil
}

func randomEndorsers(count int, totalPeers []*Peer) Endorsers {
	var endorsers []*Peer
	for _, index := range util.GetRandomIndices(count, len(totalPeers)-1) {
		endorsers = append(endorsers, totalPeers[index])
	}
	return endorsers
}

type endorsementDescriptor struct {
	endorsersByGroups map[string][]*Peer
	layouts           []map[string]int
}

func NewClient(createConnection Dialer, authInfo *discovery.AuthInfo, s Signer) *client {
	return &client{
		createConnection: createConnection,
		authInfo:         authInfo,
		signRequest:      s,
	}
}
