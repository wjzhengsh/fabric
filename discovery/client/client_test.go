/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package discovery

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/golang/protobuf/proto"
	"github.com/hyperledger/fabric/common/cauthdsl"
	"github.com/hyperledger/fabric/common/chaincode"
	"github.com/hyperledger/fabric/common/policies"
	"github.com/hyperledger/fabric/common/util"
	"github.com/hyperledger/fabric/core/comm"
	fabricdisc "github.com/hyperledger/fabric/discovery"
	"github.com/hyperledger/fabric/discovery/endorsement"
	"github.com/hyperledger/fabric/gossip/api"
	gossipcommon "github.com/hyperledger/fabric/gossip/common"
	discovery3 "github.com/hyperledger/fabric/gossip/discovery"
	"github.com/hyperledger/fabric/protos/common"
	"github.com/hyperledger/fabric/protos/discovery"
	"github.com/hyperledger/fabric/protos/gossip"
	"github.com/hyperledger/fabric/protos/msp"
	"github.com/hyperledger/fabric/protos/utils"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

var (
	ctx = context.Background()

	orgCombinationsThatSatisfyPolicy = [][]string{
		{"A", "B"}, {"C"}, {"A", "D"},
	}

	expectedOrgCombinations = []map[string]struct{}{
		{
			"A": {},
			"B": {},
		},
		{
			"C": {},
		},
		{
			"A": {},
			"D": {},
		},
	}

	cc = &gossip.Chaincode{
		Name:    "mycc",
		Version: "1.0",
	}

	propertiesWithChaincodes = &gossip.Properties{
		Chaincodes: []*gossip.Chaincode{cc},
	}

	expectedConf = &discovery.ConfigResult{
		Msps: map[string]*msp.FabricMSPConfig{
			"A": {},
			"B": {},
			"C": {},
			"D": {},
		},
		Orderers: map[string]*discovery.Endpoints{
			"A": {},
			"B": {},
		},
	}

	channelPeersWithChaincodes = discovery3.Members{
		newPeer(0, stateInfoMessage(cc), propertiesWithChaincodes).NetworkMember,
		newPeer(1, stateInfoMessage(cc), propertiesWithChaincodes).NetworkMember,
		newPeer(2, stateInfoMessage(cc), propertiesWithChaincodes).NetworkMember,
		newPeer(3, stateInfoMessage(cc), propertiesWithChaincodes).NetworkMember,
		newPeer(4, stateInfoMessage(cc), propertiesWithChaincodes).NetworkMember,
		newPeer(5, stateInfoMessage(cc), propertiesWithChaincodes).NetworkMember,
		newPeer(6, stateInfoMessage(cc), propertiesWithChaincodes).NetworkMember,
		newPeer(7, stateInfoMessage(cc), propertiesWithChaincodes).NetworkMember,
	}

	channelPeersWithoutChaincodes = discovery3.Members{
		newPeer(0, stateInfoMessage(), nil).NetworkMember,
		newPeer(1, stateInfoMessage(), nil).NetworkMember,
		newPeer(2, stateInfoMessage(), nil).NetworkMember,
		newPeer(3, stateInfoMessage(), nil).NetworkMember,
		newPeer(4, stateInfoMessage(), nil).NetworkMember,
		newPeer(5, stateInfoMessage(), nil).NetworkMember,
		newPeer(6, stateInfoMessage(), nil).NetworkMember,
		newPeer(7, stateInfoMessage(), nil).NetworkMember,
	}

	membershipPeers = discovery3.Members{
		newPeer(0, aliveMessage(0), nil).NetworkMember,
		newPeer(1, aliveMessage(1), nil).NetworkMember,
		newPeer(2, aliveMessage(2), nil).NetworkMember,
		newPeer(3, aliveMessage(3), nil).NetworkMember,
		newPeer(4, aliveMessage(4), nil).NetworkMember,
		newPeer(5, aliveMessage(5), nil).NetworkMember,
		newPeer(6, aliveMessage(6), nil).NetworkMember,
		newPeer(7, aliveMessage(7), nil).NetworkMember,
	}

	peerIdentities = api.PeerIdentitySet{
		peerIdentity("A", 0),
		peerIdentity("A", 1),
		peerIdentity("B", 2),
		peerIdentity("B", 3),
		peerIdentity("C", 4),
		peerIdentity("C", 5),
		peerIdentity("D", 6),
		peerIdentity("D", 7),
	}

	resultsWithoutEnvelopes = &discovery.QueryResult_CcQueryRes{
		CcQueryRes: &discovery.ChaincodeQueryResult{
			Content: []*discovery.EndorsementDescriptor{
				{
					Chaincode: "mycc",
					EndorsersByGroups: map[string]*discovery.Peers{
						"A": {
							Peers: []*discovery.Peer{
								{},
							},
						},
					},
					Layouts: []*discovery.Layout{
						{
							QuantitiesByGroup: map[string]uint32{},
						},
					},
				},
			},
		},
	}

	resultsWithEnvelopesButWithInsufficientPeers = &discovery.QueryResult_CcQueryRes{
		CcQueryRes: &discovery.ChaincodeQueryResult{
			Content: []*discovery.EndorsementDescriptor{
				{
					Chaincode: "mycc",
					EndorsersByGroups: map[string]*discovery.Peers{
						"A": {
							Peers: []*discovery.Peer{
								{
									StateInfo:      stateInfoMessage(),
									MembershipInfo: aliveMessage(0),
									Identity:       peerIdentity("A", 0).Identity,
								},
							},
						},
					},
					Layouts: []*discovery.Layout{
						{
							QuantitiesByGroup: map[string]uint32{
								"A": 2,
							},
						},
					},
				},
			},
		},
	}

	resultsWithEnvelopesButWithMismatchedLayout = &discovery.QueryResult_CcQueryRes{
		CcQueryRes: &discovery.ChaincodeQueryResult{
			Content: []*discovery.EndorsementDescriptor{
				{
					Chaincode: "mycc",
					EndorsersByGroups: map[string]*discovery.Peers{
						"A": {
							Peers: []*discovery.Peer{
								{
									StateInfo:      stateInfoMessage(),
									MembershipInfo: aliveMessage(0),
									Identity:       peerIdentity("A", 0).Identity,
								},
							},
						},
					},
					Layouts: []*discovery.Layout{
						{
							QuantitiesByGroup: map[string]uint32{
								"B": 2,
							},
						},
					},
				},
			},
		},
	}
)

func loadFileOrPanic(file string) []byte {
	b, err := ioutil.ReadFile(file)
	if err != nil {
		panic(err)
	}
	return b
}

func createGRPCServer(t *testing.T) *comm.GRPCServer {
	serverCert := loadFileOrPanic(filepath.Join("testdata", "server", "cert.pem"))
	serverKey := loadFileOrPanic(filepath.Join("testdata", "server", "key.pem"))
	s, err := comm.NewGRPCServer("localhost:0", comm.ServerConfig{
		SecOpts: &comm.SecureOptions{
			UseTLS:      true,
			Certificate: serverCert,
			Key:         serverKey,
		},
	})
	assert.NoError(t, err)
	return s
}

func createConnector(t *testing.T, certificate tls.Certificate, targetPort int) func() (*grpc.ClientConn, error) {
	caCert := loadFileOrPanic(filepath.Join("testdata", "server", "ca.pem"))
	tlsConf := &tls.Config{
		RootCAs:      x509.NewCertPool(),
		Certificates: []tls.Certificate{certificate},
	}
	tlsConf.RootCAs.AppendCertsFromPEM(caCert)

	addr := fmt.Sprintf("localhost:%d", targetPort)
	return func() (*grpc.ClientConn, error) {
		conn, err := grpc.Dial(addr, grpc.WithBlock(), grpc.WithTransportCredentials(credentials.NewTLS(tlsConf)))
		assert.NoError(t, err)
		if err != nil {
			panic(err)
		}
		return conn, nil
	}
}

func createDiscoveryService(sup *mockSupport) discovery.DiscoveryServer {
	mdf := &ccMetadataFetcher{}
	pe := &principalEvaluator{}
	pf := &policyFetcher{}

	sig, _ := cauthdsl.FromString("OR(AND('A.member', 'B.member'), 'C.member', AND('A.member', 'D.member'))")
	polBytes, _ := proto.Marshal(sig)
	mdf.On("ChaincodeMetadata").Return(&chaincode.InstantiatedChaincode{
		Policy:  polBytes,
		Name:    "mycc",
		Version: "1.0",
		Id:      []byte{1, 2, 3},
	})
	pf.On("PolicyByChaincode").Return(&inquireablePolicy{
		orgCombinations: orgCombinationsThatSatisfyPolicy,
	})
	sup.On("Config", "mychannel").Return(expectedConf)
	sup.On("Peers").Return(membershipPeers)
	sup.endorsementAnalyzer = endorsement.NewEndorsementAnalyzer(sup, pf, pe, mdf)
	sup.On("IdentityInfo").Return(peerIdentities)
	return fabricdisc.NewService(true, sup)
}

func TestClient(t *testing.T) {
	clientCert := loadFileOrPanic(filepath.Join("testdata", "client", "cert.pem"))
	clientKey := loadFileOrPanic(filepath.Join("testdata", "client", "key.pem"))
	clientTLSCert, err := tls.X509KeyPair(clientCert, clientKey)
	assert.NoError(t, err)
	server := createGRPCServer(t)
	sup := &mockSupport{}
	service := createDiscoveryService(sup)
	discovery.RegisterDiscoveryServer(server.Server(), service)
	go server.Start()

	_, portStr, _ := net.SplitHostPort(server.Address())
	port, _ := strconv.ParseInt(portStr, 10, 64)
	connect := createConnector(t, clientTLSCert, int(port))

	signer := func(msg []byte) ([]byte, error) {
		return msg, nil
	}

	cl := NewClient(connect, &discovery.AuthInfo{
		ClientIdentity:    []byte{1, 2, 3},
		ClientTlsCertHash: util.ComputeSHA256(clientTLSCert.Certificate[0]),
	}, signer)

	sup.On("PeersOfChannel").Return(channelPeersWithoutChaincodes).Times(2)
	req := NewRequest()
	req.OfChannel("mychannel").AddEndorsersQuery("mycc").AddPeersQuery().AddConfigQuery()
	r, err := cl.Send(ctx, req)
	assert.NoError(t, err)

	// Check behavior for channels that we didn't query for.
	fakeChannel := r.ForChannel("fakeChannel")
	peers, err := fakeChannel.Peers()
	assert.Equal(t, ErrNotFound, err)
	assert.Nil(t, peers)

	endorsers, err := fakeChannel.Endorsers("mycc")
	assert.Equal(t, ErrNotFound, err)
	assert.Nil(t, endorsers)

	conf, err := fakeChannel.Config()
	assert.Equal(t, ErrNotFound, err)
	assert.Nil(t, conf)

	// Check response for the correct channel
	mychannel := r.ForChannel("mychannel")
	conf, err = mychannel.Config()
	assert.NoError(t, err)
	assert.Equal(t, expectedConf.Msps, conf.Msps)
	assert.Equal(t, expectedConf.Orderers, conf.Orderers)

	peers, err = mychannel.Peers()
	assert.NoError(t, err)
	// We should see all peers as provided above
	assert.Len(t, peers, 8)

	endorsers, err = mychannel.Endorsers("mycc")
	// However, since we didn't provide any chaincodes to these peers - the server shouldn't
	// be able to construct the descriptor.
	// Just check that the appropriate error is returned, and nothing crashes.
	assert.Contains(t, err.Error(), "failed constructing descriptor for chaincode")
	assert.Nil(t, endorsers)

	// Next, we check the case when the peers publish chaincode for themselves.
	sup.On("PeersOfChannel").Return(channelPeersWithChaincodes).Times(2)
	req = NewRequest()
	req.OfChannel("mychannel").AddEndorsersQuery("mycc").AddPeersQuery()
	r, err = cl.Send(ctx, req)
	assert.NoError(t, err)

	mychannel = r.ForChannel("mychannel")
	peers, err = mychannel.Peers()
	assert.NoError(t, err)
	assert.Len(t, peers, 8)

	// We should get a valid endorsement descriptor from the service
	endorsers, err = mychannel.Endorsers("mycc")
	assert.NoError(t, err)
	// The combinations of endorsers should be in the expected combinations
	assert.Contains(t, expectedOrgCombinations, getMSPs(endorsers))
}

func TestUnableToSign(t *testing.T) {
	signer := func(msg []byte) ([]byte, error) {
		return nil, errors.New("not enough entropy")
	}
	failToConnect := func() (*grpc.ClientConn, error) {
		return nil, nil
	}
	cl := NewClient(failToConnect, &discovery.AuthInfo{
		ClientIdentity: []byte{1, 2, 3},
	}, signer)
	req := NewRequest()
	req = req.OfChannel("mychannel")
	resp, err := cl.Send(ctx, req)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "not enough entropy")
}

func TestUnableToConnect(t *testing.T) {
	signer := func(msg []byte) ([]byte, error) {
		return msg, nil
	}
	failToConnect := func() (*grpc.ClientConn, error) {
		return nil, errors.New("unable to connect")
	}
	cl := NewClient(failToConnect, &discovery.AuthInfo{
		ClientIdentity: []byte{1, 2, 3},
	}, signer)
	req := NewRequest()
	req = req.OfChannel("mychannel")
	resp, err := cl.Send(ctx, req)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "unable to connect")
}

func TestBadResponses(t *testing.T) {
	signer := func(msg []byte) ([]byte, error) {
		return msg, nil
	}
	svc := newMockDiscoveryService()
	t.Logf("Started mock discovery service on port %d", svc.port)
	defer svc.shutdown()

	connect := func() (*grpc.ClientConn, error) {
		return grpc.Dial(fmt.Sprintf("localhost:%d", svc.port), grpc.WithInsecure())
	}

	cl := NewClient(connect, &discovery.AuthInfo{
		ClientIdentity: []byte{1, 2, 3},
	}, signer)

	// Scenario I: discovery service sends back an error
	svc.On("Discover").Return(nil, errors.New("foo")).Once()
	req := NewRequest()
	req.OfChannel("mychannel").AddEndorsersQuery("mycc").AddPeersQuery().AddConfigQuery()
	r, err := cl.Send(ctx, req)
	assert.Contains(t, err.Error(), "foo")
	assert.Nil(t, r)

	// Scenario II: discovery service sends back an empty response
	svc.On("Discover").Return(&discovery.Response{}, nil).Once()
	req = NewRequest()
	req.OfChannel("mychannel").AddEndorsersQuery("mycc").AddPeersQuery().AddConfigQuery()
	r, err = cl.Send(ctx, req)
	assert.Equal(t, "Sent 3 queries but received 0 responses back", err.Error())
	assert.Nil(t, r)

	// Scenario III: discovery service sends back a layout for the wrong chaincode
	svc.On("Discover").Return(&discovery.Response{
		Results: []*discovery.QueryResult{
			{
				Result: &discovery.QueryResult_CcQueryRes{
					CcQueryRes: &discovery.ChaincodeQueryResult{
						Content: []*discovery.EndorsementDescriptor{
							{
								Chaincode: "notmycc",
							},
						},
					},
				},
			},
		},
	}, nil).Once()
	req = NewRequest()
	req.OfChannel("mychannel").AddEndorsersQuery("mycc")
	r, err = cl.Send(ctx, req)
	assert.NoError(t, err)
	mychannel := r.ForChannel("mychannel")
	endorsers, err := mychannel.Endorsers("mycc")
	assert.Nil(t, endorsers)
	assert.Equal(t, ErrNotFound, err)

	// Scenario IV: discovery service sends back a layout that has empty envelopes
	svc.On("Discover").Return(&discovery.Response{
		Results: []*discovery.QueryResult{
			{
				Result: resultsWithoutEnvelopes,
			},
		},
	}, nil).Once()
	req = NewRequest()
	req.OfChannel("mychannel").AddEndorsersQuery("mycc")
	r, err = cl.Send(ctx, req)
	assert.Contains(t, err.Error(), "received empty envelope(s) for endorsers for chaincode mycc")
	assert.Nil(t, r)

	// Scenario V: discovery service sends back a layout that has a group that requires more
	// members than are present.
	svc.On("Discover").Return(&discovery.Response{
		Results: []*discovery.QueryResult{
			{
				Result: resultsWithEnvelopesButWithInsufficientPeers,
			},
		},
	}, nil).Once()
	req = NewRequest()
	req.OfChannel("mychannel").AddEndorsersQuery("mycc")
	r, err = cl.Send(ctx, req)
	assert.NoError(t, err)
	mychannel = r.ForChannel("mychannel")
	endorsers, err = mychannel.Endorsers("mycc")
	assert.Nil(t, endorsers)
	assert.Contains(t, err.Error(), "layout has a group that requires at least 2 peers, but only 0 peers are known")

	// Scenario VI: discovery service sends back a layout that has a group that doesn't have a matching peer set
	svc.On("Discover").Return(&discovery.Response{
		Results: []*discovery.QueryResult{
			{
				Result: resultsWithEnvelopesButWithMismatchedLayout,
			},
		},
	}, nil).Once()
	req = NewRequest()
	req.OfChannel("mychannel").AddEndorsersQuery("mycc")
	r, err = cl.Send(ctx, req)
	assert.Contains(t, err.Error(), "group B isn't mapped to endorsers, but exists in a layout")
	assert.Empty(t, r)
}

func getMSP(peer *Peer) string {
	endpoint := peer.AliveMessage.GetAliveMsg().Membership.Endpoint
	id, _ := strconv.ParseInt(endpoint[1:], 10, 64)
	switch id / 2 {
	case 0:
		return "A"
	case 1:
		return "B"
	case 2:
		return "C"
	default:
		return "D"
	}
}

func getMSPs(endorsers []*Peer) map[string]struct{} {
	m := make(map[string]struct{})
	for _, endorser := range endorsers {
		m[getMSP(endorser)] = struct{}{}
	}
	return m
}

type ccMetadataFetcher struct {
	mock.Mock
}

func (mdf ccMetadataFetcher) ChaincodeMetadata(channel string, cc string) *chaincode.InstantiatedChaincode {
	return mdf.Called().Get(0).(*chaincode.InstantiatedChaincode)
}

type principalEvaluator struct {
}

func (pe *principalEvaluator) SatisfiesPrincipal(channel string, identity []byte, principal *msp.MSPPrincipal) error {
	sID := &msp.SerializedIdentity{}
	proto.Unmarshal(identity, sID)
	p := &msp.MSPRole{}
	proto.Unmarshal(principal.Principal, p)
	if sID.Mspid == p.MspIdentifier {
		return nil
	}
	return errors.Errorf("peer %s has MSP %s but should have MSP %s", string(sID.IdBytes), sID.Mspid, p.MspIdentifier)
}

type policyFetcher struct {
	mock.Mock
}

func (pf *policyFetcher) PolicyByChaincode(channel string, cc string) policies.InquireablePolicy {
	return pf.Called().Get(0).(policies.InquireablePolicy)
}

type endorsementAnalyzer interface {
	PeersForEndorsement(chaincode string, chainID gossipcommon.ChainID) (*discovery.EndorsementDescriptor, error)
}

type inquireablePolicy struct {
	principals      []*msp.MSPPrincipal
	orgCombinations [][]string
}

func (ip *inquireablePolicy) appendPrincipal(orgName string) {
	ip.principals = append(ip.principals, &msp.MSPPrincipal{
		PrincipalClassification: msp.MSPPrincipal_ROLE,
		Principal:               utils.MarshalOrPanic(&msp.MSPRole{Role: msp.MSPRole_MEMBER, MspIdentifier: orgName})})
}

func (ip *inquireablePolicy) SatisfiedBy() []policies.PrincipalSet {
	var res []policies.PrincipalSet
	for _, orgs := range ip.orgCombinations {
		for _, org := range orgs {
			ip.appendPrincipal(org)
		}
		res = append(res, ip.principals)
		ip.principals = nil
	}
	return res
}

func peerIdentity(mspID string, i int) api.PeerIdentityInfo {
	p := []byte(fmt.Sprintf("p%d", i))
	sId := &msp.SerializedIdentity{
		Mspid:   mspID,
		IdBytes: p,
	}
	b, _ := proto.Marshal(sId)
	return api.PeerIdentityInfo{
		Identity:     api.PeerIdentityType(b),
		PKIId:        gossipcommon.PKIidType(p),
		Organization: api.OrgIdentityType(mspID),
	}
}

type peerInfo struct {
	identity api.PeerIdentityType
	pkiID    gossipcommon.PKIidType
	discovery3.NetworkMember
}

func aliveMessage(id int) *gossip.Envelope {
	g := &gossip.GossipMessage{
		Content: &gossip.GossipMessage_AliveMsg{
			AliveMsg: &gossip.AliveMessage{
				Membership: &gossip.Member{
					Endpoint: fmt.Sprintf("p%d", id),
				},
			},
		},
	}
	sMsg, _ := g.NoopSign()
	return sMsg.Envelope
}

func stateInfoMessage(chaincodes ...*gossip.Chaincode) *gossip.Envelope {
	g := &gossip.GossipMessage{
		Content: &gossip.GossipMessage_StateInfo{
			StateInfo: &gossip.StateInfo{
				Properties: &gossip.Properties{
					Chaincodes: chaincodes,
				},
			},
		},
	}
	sMsg, _ := g.NoopSign()
	return sMsg.Envelope
}

func newPeer(i int, env *gossip.Envelope, properties *gossip.Properties) *peerInfo {
	p := fmt.Sprintf("p%d", i)
	return &peerInfo{
		pkiID:    gossipcommon.PKIidType(p),
		identity: api.PeerIdentityType(p),
		NetworkMember: discovery3.NetworkMember{
			PKIid:            gossipcommon.PKIidType(p),
			Endpoint:         p,
			InternalEndpoint: p,
			Envelope:         env,
			Properties:       properties,
		},
	}
}

type mockSupport struct {
	seq uint64
	mock.Mock
	endorsementAnalyzer
}

func (ms *mockSupport) ConfigSequence(channel string) uint64 {
	// Ensure cache is bypassed
	ms.seq++
	return ms.seq
}

func (ms *mockSupport) IdentityInfo() api.PeerIdentitySet {
	return ms.Called().Get(0).(api.PeerIdentitySet)
}

func (*mockSupport) ChannelExists(channel string) bool {
	return true
}

func (ms *mockSupport) PeersOfChannel(gossipcommon.ChainID) discovery3.Members {
	return ms.Called().Get(0).(discovery3.Members)
}

func (ms *mockSupport) Peers() discovery3.Members {
	return ms.Called().Get(0).(discovery3.Members)
}

func (ms *mockSupport) PeersForEndorsement(chaincode string, channel gossipcommon.ChainID) (*discovery.EndorsementDescriptor, error) {
	return ms.endorsementAnalyzer.PeersForEndorsement(chaincode, channel)
}

func (*mockSupport) EligibleForService(channel string, data common.SignedData) error {
	return nil
}

func (ms *mockSupport) Config(channel string) (*discovery.ConfigResult, error) {
	return ms.Called(channel).Get(0).(*discovery.ConfigResult), nil
}

type mockDiscoveryServer struct {
	mock.Mock
	*grpc.Server
	port int64
}

func newMockDiscoveryService() *mockDiscoveryServer {
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		panic(err)
	}
	s := grpc.NewServer()
	d := &mockDiscoveryServer{
		Server: s,
	}
	discovery.RegisterDiscoveryServer(s, d)
	go s.Serve(l)
	_, portStr, _ := net.SplitHostPort(l.Addr().String())
	d.port, _ = strconv.ParseInt(portStr, 10, 64)
	return d
}

func (ds *mockDiscoveryServer) shutdown() {
	ds.Server.Stop()
}

func (ds *mockDiscoveryServer) Discover(context.Context, *discovery.SignedRequest) (*discovery.Response, error) {
	args := ds.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*discovery.Response), nil
}
