package logic

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gravitl/netmaker/database"
	"github.com/gravitl/netmaker/logger"
	"github.com/gravitl/netmaker/models"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// GetExtPeersList - gets the ext peers lists
func GetExtPeersList(node *models.Node) ([]models.ExtPeersResponse, error) {

	var peers []models.ExtPeersResponse
	records, err := database.FetchRecords(database.EXT_CLIENT_TABLE_NAME)

	if err != nil {
		return peers, err
	}

	for _, value := range records {
		var peer models.ExtPeersResponse
		var extClient models.ExtClient
		err = json.Unmarshal([]byte(value), &peer)
		if err != nil {
			logger.Log(2, "failed to unmarshal peer when getting ext peer list")
			continue
		}
		err = json.Unmarshal([]byte(value), &extClient)
		if err != nil {
			logger.Log(2, "failed to unmarshal ext client")
			continue
		}

		if extClient.Enabled && extClient.Network == node.Network && extClient.IngressGatewayID == node.ID.String() {
			peers = append(peers, peer)
		}
	}
	return peers, err
}

// ExtClient.GetEgressRangesOnNetwork - returns the egress ranges on network of ext client
func GetEgressRangesOnNetwork(client *models.ExtClient) ([]string, error) {

	var result []string
	nodesData, err := GetAllNodes()
	if err != nil {
		return []string{}, err
	}
	for _, currentNode := range nodesData {
		if currentNode.Network != client.Network {
			continue
		}
		if currentNode.IsEgressGateway { // add the egress gateway range(s) to the result
			if len(currentNode.EgressGatewayRanges) > 0 {
				result = append(result, currentNode.EgressGatewayRanges...)
			}
		}
	}

	return result, nil
}

// DeleteExtClient - deletes an existing ext client
func DeleteExtClient(network string, clientid string) error {
	key, err := GetRecordKey(clientid, network)
	if err != nil {
		return err
	}
	// invalidate cache
	CacheExtClientsMutex.Lock()
	CacheExtClients = nil
	CacheExtClientsMutex.Unlock()
	err = database.DeleteRecord(database.EXT_CLIENT_TABLE_NAME, key)
	return err
}

// GetNetworkExtClients - gets the ext clients of given network
func GetNetworkExtClients(network string) ([]models.ExtClient, error) {
	var extclients []models.ExtClient

	records, err := fetchExtClients()
	if err != nil {
		return extclients, err
	}
	for _, extclient := range records {
		if extclient.Network == network {
			extclients = append(extclients, extclient)
		}
	}
	return extclients, err
}

// GetExtClient - gets a single ext client on a network
func GetExtClient(clientid string, network string) (models.ExtClient, error) {
	// TODO cache
	var extclient models.ExtClient
	key, err := GetRecordKey(clientid, network)
	if err != nil {
		return extclient, err
	}
	data, err := database.FetchRecord(database.EXT_CLIENT_TABLE_NAME, key)
	if err != nil {
		return extclient, err
	}
	err = json.Unmarshal([]byte(data), &extclient)

	return extclient, err
}

// GetExtClient - gets a single ext client on a network
func GetExtClientByPubKey(publicKey string, network string) (*models.ExtClient, error) {
	netClients, err := GetNetworkExtClients(network)
	if err != nil {
		return nil, err
	}
	for i := range netClients {
		ec := netClients[i]
		if ec.PublicKey == publicKey {
			return &ec, nil
		}
	}

	return nil, fmt.Errorf("no client found")
}

// CreateExtClient - creates an extclient
func CreateExtClient(extclient *models.ExtClient) error {

	if len(extclient.PublicKey) == 0 {
		privateKey, err := wgtypes.GeneratePrivateKey()
		if err != nil {
			return err
		}
		extclient.PrivateKey = privateKey.String()
		extclient.PublicKey = privateKey.PublicKey().String()
	} else if len(extclient.PrivateKey) == 0 && len(extclient.PublicKey) > 0 {
		extclient.PrivateKey = "[ENTER PRIVATE KEY]"
	}

	parentNetwork, err := GetNetwork(extclient.Network)
	if err != nil {
		return err
	}
	if extclient.Address == "" {
		if parentNetwork.IsIPv4 == "yes" {
			newAddress, err := UniqueAddress(extclient.Network, true)
			if err != nil {
				return err
			}
			extclient.Address = newAddress.String()
		}
	}

	if extclient.Address6 == "" {
		if parentNetwork.IsIPv6 == "yes" {
			addr6, err := UniqueAddress6(extclient.Network, true)
			if err != nil {
				return err
			}
			extclient.Address6 = addr6.String()
		}
	}

	if extclient.ClientID == "" {
		extclient.ClientID = models.GenerateNodeName()
	}

	extclient.LastModified = time.Now().Unix()
	return SaveExtClient(extclient)
}

// SaveExtClient - saves an ext client to database
func SaveExtClient(extclient *models.ExtClient) error {
	key, err := GetRecordKey(extclient.ClientID, extclient.Network)
	if err != nil {
		return err
	}
	data, err := json.Marshal(&extclient)
	if err != nil {
		return err
	}
	// invalidate cache
	CacheExtClientsMutex.Lock()
	CacheExtClients = nil
	CacheExtClientsMutex.Unlock()
	if err = database.Insert(key, string(data), database.EXT_CLIENT_TABLE_NAME); err != nil {
		return err
	}
	return SetNetworkNodesLastModified(extclient.Network)
}

// UpdateExtClient - updates an ext client with new values
func UpdateExtClient(old *models.ExtClient, update *models.CustomExtClient) (*models.ExtClient, error) {
	new := old
	err := DeleteExtClient(old.Network, old.ClientID)
	if err != nil {
		return new, err
	}
	new.ClientID = update.ClientID
	if update.PublicKey != "" && old.PublicKey != update.PublicKey {
		new.PublicKey = update.PublicKey
	}
	if update.DNS != "" && update.DNS != old.DNS {
		new.DNS = update.DNS
	}
	if update.Enabled != old.Enabled {
		new.Enabled = update.Enabled
	}
	if update.ExtraAllowedIPs != nil && StringDifference(old.ExtraAllowedIPs, update.ExtraAllowedIPs) != nil {
		new.ExtraAllowedIPs = update.ExtraAllowedIPs
	}
	return new, CreateExtClient(new)
}

// GetExtClientsByID - gets the clients of attached gateway
func GetExtClientsByID(nodeid, network string) ([]models.ExtClient, error) {
	var result []models.ExtClient
	currentClients, err := GetNetworkExtClients(network)
	if err != nil {
		return result, err
	}
	for i := range currentClients {
		if currentClients[i].IngressGatewayID == nodeid {
			result = append(result, currentClients[i])
		}
	}
	return result, nil
}

// GetAllExtClients - gets all ext clients from DB
func GetAllExtClients() ([]models.ExtClient, error) {
	var clients = []models.ExtClient{}
	currentNetworks, err := GetNetworks()
	if err != nil && database.IsEmptyRecord(err) {
		return clients, nil
	} else if err != nil {
		return clients, err
	}

	for i := range currentNetworks {
		netName := currentNetworks[i].NetID
		netClients, err := GetNetworkExtClients(netName)
		if err != nil {
			continue
		}
		clients = append(clients, netClients...)
	}

	return clients, nil
}

var CacheExtClients []models.ExtClient
var CacheExtClientsMutex = sync.RWMutex{}

func fetchExtClients() ([]models.ExtClient, error) {
	// cache read
	CacheExtClientsMutex.RLock()
	if CacheExtClients != nil {
		defer CacheExtClientsMutex.RUnlock()
		return CacheExtClients, nil
	}
	CacheExtClientsMutex.RUnlock()
	// fetch
	var extclients []models.ExtClient
	records, err := database.FetchRecords(database.EXT_CLIENT_TABLE_NAME)
	if err != nil {
		return extclients, err
	}
	for _, value := range records {
		var extclient models.ExtClient
		err = json.Unmarshal([]byte(value), &extclient)
		if err != nil {
			continue
		}
	}
	// cache write
	CacheExtClientsMutex.Lock()
	CacheExtClients = extclients
	CacheExtClientsMutex.Unlock()
	return extclients, err
}
