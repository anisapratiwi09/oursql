package consensus

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"math/rand"
	"strings"
	"time"

	"github.com/fatih/structs"
	"github.com/gelembjuk/oursql/lib"
	"github.com/gelembjuk/oursql/lib/net"
	"github.com/gelembjuk/oursql/lib/utils"
	"github.com/gelembjuk/oursql/node/dbquery"
	"github.com/gelembjuk/oursql/node/structures"
	"github.com/mitchellh/mapstructure"
)

const (
	KindConseususPoW = "proofofwork"
)

type ConsensusConfigCost struct {
	Default     float64
	RowDelete   float64
	RowUpdate   float64
	RowInsert   float64
	TableCreate float64
}
type ConsensusConfigTable struct {
	Table            string
	AllowRowDelete   bool
	AllowRowUpdate   bool
	AllowRowInsert   bool
	AllowTableCreate bool
	TransactionCost  ConsensusConfigCost
}
type ConsensusConfigApplication struct {
	Name    string
	WebSite string
	Team    string
}
type consensusConfigState struct {
	isDefault bool
	filePath  string
}
type ConsensusConfig struct {
	Application            ConsensusConfigApplication
	Kind                   string
	CoinsForBlockMade      float64
	Settings               map[string]interface{}
	AllowTableCreate       bool
	AllowTableDrop         bool
	AllowRowDelete         bool
	TransactionCost        ConsensusConfigCost
	UnmanagedTables        []string
	TableRules             []ConsensusConfigTable
	InitNodesAddreses      []string
	PaidTransactionsWallet string
	state                  consensusConfigState
}

// Load config from config file. Some config options an be missed
// missed options must be replaced with default values correctly
func NewConfigFromFile(filepath string) (*ConsensusConfig, error) {
	config := ConsensusConfig{}

	err := config.loadFromFile(filepath)

	if err != nil {
		return nil, err
	}

	return &config, nil
}

func NewConfigDefault() (*ConsensusConfig, error) {
	c := ConsensusConfig{}
	c.Kind = KindConseususPoW
	c.CoinsForBlockMade = 10
	c.AllowTableCreate = true
	c.AllowTableDrop = true
	c.AllowRowDelete = true
	c.UnmanagedTables = []string{}
	c.TableRules = []ConsensusConfigTable{}
	c.InitNodesAddreses = []string{}

	// make defauls PoW settings
	s := ProofOfWorkSettings{}
	s.completeSettings()

	c.Settings = structs.Map(s)

	c.state.isDefault = true
	c.state.filePath = ""

	return &c, nil
}

func (c *ConsensusConfig) loadFromFile(filepath string) error {

	jsonStr, err := ioutil.ReadFile(filepath)

	if err != nil {
		// error is bad only if file exists but we can not open to read
		return err
	}

	err = c.load(jsonStr)

	if err != nil {
		return err
	}
	c.state.isDefault = false
	c.state.filePath = filepath

	return nil
}

func (c *ConsensusConfig) load(jsonStr []byte) error {

	err := json.Unmarshal(jsonStr, c)

	if err != nil {

		return err
	}

	if c.CoinsForBlockMade == 0 {
		c.CoinsForBlockMade = 10
	}

	if c.Kind == "" {
		c.Kind = KindConseususPoW
	}
	if c.Kind == KindConseususPoW {
		// check all PoW settings are done
		s := ProofOfWorkSettings{}

		mapstructure.Decode(c.Settings, &s)

		s.completeSettings()

		c.Settings = structs.Map(s)
	}

	return nil
}

// Return info about transaction settings
func (cc ConsensusConfig) GetInfoForTransactions() structures.ConsensusInfo {
	return structures.ConsensusInfo{cc.CoinsForBlockMade}
}

// Exports config to file
func (cc ConsensusConfig) ExportToFile(filepath string, defaultaddresses string, appname string, thisnodeaddr string) error {
	jsondata, err := cc.Export(defaultaddresses, appname, thisnodeaddr)

	if err != nil {
		return err
	}

	err = ioutil.WriteFile(filepath, jsondata, 0644)

	return err
}

// Exports config to JSON string
func (cc ConsensusConfig) Export(defaultaddresses string, appname string, thisnodeaddr string) (jsondata []byte, err error) {
	addresses := []string{}

	if defaultaddresses != "" {
		list := strings.Split(defaultaddresses, ",")

		for _, a := range list {
			if a == "" {
				continue
			}
			if a == "own" {
				if thisnodeaddr != "" {
					a = thisnodeaddr
				} else {
					continue
				}
			}
			addresses = append(addresses, a)
		}
	}

	if len(addresses) > 0 {
		cc.InitNodesAddreses = addresses
	}

	if len(cc.InitNodesAddreses) == 0 && thisnodeaddr != "" {
		cc.InitNodesAddreses = []string{thisnodeaddr}
	}

	if len(cc.InitNodesAddreses) == 0 {
		err = errors.New("List of default addresses is empty")
		return
	}

	if appname != "" {
		cc.Application.Name = appname
	}

	if cc.Application.Name == "" {
		err = errors.New("Application name is empty. It is required")
		return
	}

	jsondata, err = json.Marshal(cc)

	return
}

// Returns one of addresses listed in initial addresses
func (cc ConsensusConfig) GetRandomInitialAddress() *net.NodeAddr {
	if len(cc.InitNodesAddreses) == 0 {
		return nil
	}
	rand.Seed(time.Now().Unix()) // initialize global pseudo random generator
	addr := cc.InitNodesAddreses[rand.Intn(len(cc.InitNodesAddreses))]

	na := net.NodeAddr{}
	na.LoadFromString(addr)

	return &na
}

// Checks if a config structure was loaded from file or not
func (cc ConsensusConfig) IsDefault() bool {

	return cc.state.isDefault
}

// Set config file path. this defines a path where a config file should be, even if it is not yet here
func (cc *ConsensusConfig) SetConfigFilePath(fp string) {
	cc.state.filePath = fp
}

// Replace consensus config file . It checks if a config is correct, if can be parsed

func (cc *ConsensusConfig) UpdateConfig(jsondoc []byte) error {

	if cc.state.filePath == "" {
		return errors.New("COnfig file path missed. Can not save")
	}

	c := ConsensusConfig{}

	err := c.load(jsondoc)

	if err != nil {
		return err
	}

	err = ioutil.WriteFile(cc.state.filePath, jsondoc, 0644)

	if err != nil {
		return err
	}

	// load this just saved contents file
	return cc.loadFromFile(cc.state.filePath)
}

// Returns wallet where to send money spent on paid transactions
func (cc ConsensusConfig) GetPaidTransactionsWallet() string {
	if cc.PaidTransactionsWallet == "" {
		return ""
	}

	pubKeyHash, err := utils.AddresToPubKeyHash(cc.PaidTransactionsWallet)

	if err != nil || len(pubKeyHash) == 0 {
		return ""
	}
	addr, err := utils.PubKeyHashToAddres(pubKeyHash)

	if err != nil {
		return ""
	}
	return addr

}

// Returns wallet where to send money spent on paid transactions
func (cc ConsensusConfig) GetPaidTransactionsWalletPubKeyHash() []byte {
	if cc.PaidTransactionsWallet == "" {
		return []byte{}
	}
	pubKeyHash, err := utils.AddresToPubKeyHash(cc.PaidTransactionsWallet)

	if err != nil || len(pubKeyHash) == 0 {
		return []byte{}
	}

	return pubKeyHash
}

// check custom rule for the table about permissions
func (cc ConsensusConfig) getTableCustomConfig(qp dbquery.QueryParsed) *ConsensusConfigTable {

	if !qp.IsUpdate() {
		return nil
	}

	if cc.TableRules == nil {
		// no any rules
		return nil
	}

	for _, t := range cc.TableRules {
		if t.Table != qp.Structure.GetTable() {
			continue
		}
		return &t
	}

	return nil
}

// check if this query requires payment for execution. return number
func (cc ConsensusConfig) checkQueryNeedsPayment(qp dbquery.QueryParsed) (float64, error) {

	// check there is custom rule for this table
	t := cc.getTableCustomConfig(qp)

	var trcost *ConsensusConfigCost

	if t != nil {
		trcost = &t.TransactionCost
	} else {
		trcost = &cc.TransactionCost
	}

	// check if current operation has a price
	if qp.Structure.GetKind() == lib.QueryKindDelete && trcost.RowDelete > 0 {

		return trcost.RowDelete, nil
	}

	if qp.Structure.GetKind() == lib.QueryKindInsert && trcost.RowInsert > 0 {

		return trcost.RowInsert, nil
	}

	if qp.Structure.GetKind() == lib.QueryKindUpdate && trcost.RowUpdate > 0 {
		return trcost.RowUpdate, nil
	}

	if qp.Structure.GetKind() == lib.QueryKindCreate && trcost.TableCreate > 0 {

		return trcost.TableCreate, nil
	}

	if trcost.Default > 0 {
		return trcost.Default, nil
	}

	return 0, nil
}