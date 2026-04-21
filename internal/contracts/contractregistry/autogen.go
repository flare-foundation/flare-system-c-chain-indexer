// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package contractregistry

import (
	"errors"
	"math/big"
	"strings"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/event"
)

// Reference imports to suppress errors if they are not otherwise used.
var (
	_ = errors.New
	_ = big.NewInt
	_ = strings.NewReader
	_ = ethereum.NotFound
	_ = bind.Bind
	_ = common.Big1
	_ = types.BloomLookup
	_ = event.NewSubscription
	_ = abi.ConvertType
)

// ContractRegistryMetaData contains all meta data concerning the ContractRegistry contract.
var ContractRegistryMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"address\",\"name\":\"_addressUpdater\",\"type\":\"address\"}],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[],\"name\":\"getAddressUpdater\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"_addressUpdater\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"getAllContracts\",\"outputs\":[{\"internalType\":\"string[]\",\"name\":\"\",\"type\":\"string[]\"},{\"internalType\":\"address[]\",\"name\":\"\",\"type\":\"address[]\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"_nameHash\",\"type\":\"bytes32\"}],\"name\":\"getContractAddressByHash\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"string\",\"name\":\"_name\",\"type\":\"string\"}],\"name\":\"getContractAddressByName\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32[]\",\"name\":\"_nameHashes\",\"type\":\"bytes32[]\"}],\"name\":\"getContractAddressesByHash\",\"outputs\":[{\"internalType\":\"address[]\",\"name\":\"\",\"type\":\"address[]\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"string[]\",\"name\":\"_names\",\"type\":\"string[]\"}],\"name\":\"getContractAddressesByName\",\"outputs\":[{\"internalType\":\"address[]\",\"name\":\"\",\"type\":\"address[]\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32[]\",\"name\":\"_contractNameHashes\",\"type\":\"bytes32[]\"},{\"internalType\":\"address[]\",\"name\":\"_contractAddresses\",\"type\":\"address[]\"}],\"name\":\"updateContractAddresses\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
}

// ContractRegistryABI is the input ABI used to generate the binding from.
// Deprecated: Use ContractRegistryMetaData.ABI instead.
var ContractRegistryABI = ContractRegistryMetaData.ABI

// ContractRegistry is an auto generated Go binding around an Ethereum contract.
type ContractRegistry struct {
	ContractRegistryCaller     // Read-only binding to the contract
	ContractRegistryTransactor // Write-only binding to the contract
	ContractRegistryFilterer   // Log filterer for contract events
}

// ContractRegistryCaller is an auto generated read-only Go binding around an Ethereum contract.
type ContractRegistryCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ContractRegistryTransactor is an auto generated write-only Go binding around an Ethereum contract.
type ContractRegistryTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ContractRegistryFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type ContractRegistryFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ContractRegistrySession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type ContractRegistrySession struct {
	Contract     *ContractRegistry // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// ContractRegistryCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type ContractRegistryCallerSession struct {
	Contract *ContractRegistryCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts           // Call options to use throughout this session
}

// ContractRegistryTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type ContractRegistryTransactorSession struct {
	Contract     *ContractRegistryTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts           // Transaction auth options to use throughout this session
}

// ContractRegistryRaw is an auto generated low-level Go binding around an Ethereum contract.
type ContractRegistryRaw struct {
	Contract *ContractRegistry // Generic contract binding to access the raw methods on
}

// ContractRegistryCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type ContractRegistryCallerRaw struct {
	Contract *ContractRegistryCaller // Generic read-only contract binding to access the raw methods on
}

// ContractRegistryTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type ContractRegistryTransactorRaw struct {
	Contract *ContractRegistryTransactor // Generic write-only contract binding to access the raw methods on
}

// NewContractRegistry creates a new instance of ContractRegistry, bound to a specific deployed contract.
func NewContractRegistry(address common.Address, backend bind.ContractBackend) (*ContractRegistry, error) {
	contract, err := bindContractRegistry(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &ContractRegistry{ContractRegistryCaller: ContractRegistryCaller{contract: contract}, ContractRegistryTransactor: ContractRegistryTransactor{contract: contract}, ContractRegistryFilterer: ContractRegistryFilterer{contract: contract}}, nil
}

// NewContractRegistryCaller creates a new read-only instance of ContractRegistry, bound to a specific deployed contract.
func NewContractRegistryCaller(address common.Address, caller bind.ContractCaller) (*ContractRegistryCaller, error) {
	contract, err := bindContractRegistry(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &ContractRegistryCaller{contract: contract}, nil
}

// NewContractRegistryTransactor creates a new write-only instance of ContractRegistry, bound to a specific deployed contract.
func NewContractRegistryTransactor(address common.Address, transactor bind.ContractTransactor) (*ContractRegistryTransactor, error) {
	contract, err := bindContractRegistry(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &ContractRegistryTransactor{contract: contract}, nil
}

// NewContractRegistryFilterer creates a new log filterer instance of ContractRegistry, bound to a specific deployed contract.
func NewContractRegistryFilterer(address common.Address, filterer bind.ContractFilterer) (*ContractRegistryFilterer, error) {
	contract, err := bindContractRegistry(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &ContractRegistryFilterer{contract: contract}, nil
}

// bindContractRegistry binds a generic wrapper to an already deployed contract.
func bindContractRegistry(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := ContractRegistryMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ContractRegistry *ContractRegistryRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ContractRegistry.Contract.ContractRegistryCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ContractRegistry *ContractRegistryRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ContractRegistry.Contract.ContractRegistryTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ContractRegistry *ContractRegistryRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ContractRegistry.Contract.ContractRegistryTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ContractRegistry *ContractRegistryCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ContractRegistry.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ContractRegistry *ContractRegistryTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ContractRegistry.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ContractRegistry *ContractRegistryTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ContractRegistry.Contract.contract.Transact(opts, method, params...)
}

// GetAddressUpdater is a free data retrieval call binding the contract method 0x5267a15d.
//
// Solidity: function getAddressUpdater() view returns(address _addressUpdater)
func (_ContractRegistry *ContractRegistryCaller) GetAddressUpdater(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _ContractRegistry.contract.Call(opts, &out, "getAddressUpdater")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// GetAddressUpdater is a free data retrieval call binding the contract method 0x5267a15d.
//
// Solidity: function getAddressUpdater() view returns(address _addressUpdater)
func (_ContractRegistry *ContractRegistrySession) GetAddressUpdater() (common.Address, error) {
	return _ContractRegistry.Contract.GetAddressUpdater(&_ContractRegistry.CallOpts)
}

// GetAddressUpdater is a free data retrieval call binding the contract method 0x5267a15d.
//
// Solidity: function getAddressUpdater() view returns(address _addressUpdater)
func (_ContractRegistry *ContractRegistryCallerSession) GetAddressUpdater() (common.Address, error) {
	return _ContractRegistry.Contract.GetAddressUpdater(&_ContractRegistry.CallOpts)
}

// GetAllContracts is a free data retrieval call binding the contract method 0x18d3ce96.
//
// Solidity: function getAllContracts() view returns(string[], address[])
func (_ContractRegistry *ContractRegistryCaller) GetAllContracts(opts *bind.CallOpts) ([]string, []common.Address, error) {
	var out []interface{}
	err := _ContractRegistry.contract.Call(opts, &out, "getAllContracts")

	if err != nil {
		return *new([]string), *new([]common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new([]string)).(*[]string)
	out1 := *abi.ConvertType(out[1], new([]common.Address)).(*[]common.Address)

	return out0, out1, err

}

// GetAllContracts is a free data retrieval call binding the contract method 0x18d3ce96.
//
// Solidity: function getAllContracts() view returns(string[], address[])
func (_ContractRegistry *ContractRegistrySession) GetAllContracts() ([]string, []common.Address, error) {
	return _ContractRegistry.Contract.GetAllContracts(&_ContractRegistry.CallOpts)
}

// GetAllContracts is a free data retrieval call binding the contract method 0x18d3ce96.
//
// Solidity: function getAllContracts() view returns(string[], address[])
func (_ContractRegistry *ContractRegistryCallerSession) GetAllContracts() ([]string, []common.Address, error) {
	return _ContractRegistry.Contract.GetAllContracts(&_ContractRegistry.CallOpts)
}

// GetContractAddressByHash is a free data retrieval call binding the contract method 0x159354a2.
//
// Solidity: function getContractAddressByHash(bytes32 _nameHash) view returns(address)
func (_ContractRegistry *ContractRegistryCaller) GetContractAddressByHash(opts *bind.CallOpts, _nameHash [32]byte) (common.Address, error) {
	var out []interface{}
	err := _ContractRegistry.contract.Call(opts, &out, "getContractAddressByHash", _nameHash)

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// GetContractAddressByHash is a free data retrieval call binding the contract method 0x159354a2.
//
// Solidity: function getContractAddressByHash(bytes32 _nameHash) view returns(address)
func (_ContractRegistry *ContractRegistrySession) GetContractAddressByHash(_nameHash [32]byte) (common.Address, error) {
	return _ContractRegistry.Contract.GetContractAddressByHash(&_ContractRegistry.CallOpts, _nameHash)
}

// GetContractAddressByHash is a free data retrieval call binding the contract method 0x159354a2.
//
// Solidity: function getContractAddressByHash(bytes32 _nameHash) view returns(address)
func (_ContractRegistry *ContractRegistryCallerSession) GetContractAddressByHash(_nameHash [32]byte) (common.Address, error) {
	return _ContractRegistry.Contract.GetContractAddressByHash(&_ContractRegistry.CallOpts, _nameHash)
}

// GetContractAddressByName is a free data retrieval call binding the contract method 0x82760fca.
//
// Solidity: function getContractAddressByName(string _name) view returns(address)
func (_ContractRegistry *ContractRegistryCaller) GetContractAddressByName(opts *bind.CallOpts, _name string) (common.Address, error) {
	var out []interface{}
	err := _ContractRegistry.contract.Call(opts, &out, "getContractAddressByName", _name)

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// GetContractAddressByName is a free data retrieval call binding the contract method 0x82760fca.
//
// Solidity: function getContractAddressByName(string _name) view returns(address)
func (_ContractRegistry *ContractRegistrySession) GetContractAddressByName(_name string) (common.Address, error) {
	return _ContractRegistry.Contract.GetContractAddressByName(&_ContractRegistry.CallOpts, _name)
}

// GetContractAddressByName is a free data retrieval call binding the contract method 0x82760fca.
//
// Solidity: function getContractAddressByName(string _name) view returns(address)
func (_ContractRegistry *ContractRegistryCallerSession) GetContractAddressByName(_name string) (common.Address, error) {
	return _ContractRegistry.Contract.GetContractAddressByName(&_ContractRegistry.CallOpts, _name)
}

// GetContractAddressesByHash is a free data retrieval call binding the contract method 0x5e11e2d1.
//
// Solidity: function getContractAddressesByHash(bytes32[] _nameHashes) view returns(address[])
func (_ContractRegistry *ContractRegistryCaller) GetContractAddressesByHash(opts *bind.CallOpts, _nameHashes [][32]byte) ([]common.Address, error) {
	var out []interface{}
	err := _ContractRegistry.contract.Call(opts, &out, "getContractAddressesByHash", _nameHashes)

	if err != nil {
		return *new([]common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new([]common.Address)).(*[]common.Address)

	return out0, err

}

// GetContractAddressesByHash is a free data retrieval call binding the contract method 0x5e11e2d1.
//
// Solidity: function getContractAddressesByHash(bytes32[] _nameHashes) view returns(address[])
func (_ContractRegistry *ContractRegistrySession) GetContractAddressesByHash(_nameHashes [][32]byte) ([]common.Address, error) {
	return _ContractRegistry.Contract.GetContractAddressesByHash(&_ContractRegistry.CallOpts, _nameHashes)
}

// GetContractAddressesByHash is a free data retrieval call binding the contract method 0x5e11e2d1.
//
// Solidity: function getContractAddressesByHash(bytes32[] _nameHashes) view returns(address[])
func (_ContractRegistry *ContractRegistryCallerSession) GetContractAddressesByHash(_nameHashes [][32]byte) ([]common.Address, error) {
	return _ContractRegistry.Contract.GetContractAddressesByHash(&_ContractRegistry.CallOpts, _nameHashes)
}

// GetContractAddressesByName is a free data retrieval call binding the contract method 0x76d2b1af.
//
// Solidity: function getContractAddressesByName(string[] _names) view returns(address[])
func (_ContractRegistry *ContractRegistryCaller) GetContractAddressesByName(opts *bind.CallOpts, _names []string) ([]common.Address, error) {
	var out []interface{}
	err := _ContractRegistry.contract.Call(opts, &out, "getContractAddressesByName", _names)

	if err != nil {
		return *new([]common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new([]common.Address)).(*[]common.Address)

	return out0, err

}

// GetContractAddressesByName is a free data retrieval call binding the contract method 0x76d2b1af.
//
// Solidity: function getContractAddressesByName(string[] _names) view returns(address[])
func (_ContractRegistry *ContractRegistrySession) GetContractAddressesByName(_names []string) ([]common.Address, error) {
	return _ContractRegistry.Contract.GetContractAddressesByName(&_ContractRegistry.CallOpts, _names)
}

// GetContractAddressesByName is a free data retrieval call binding the contract method 0x76d2b1af.
//
// Solidity: function getContractAddressesByName(string[] _names) view returns(address[])
func (_ContractRegistry *ContractRegistryCallerSession) GetContractAddressesByName(_names []string) ([]common.Address, error) {
	return _ContractRegistry.Contract.GetContractAddressesByName(&_ContractRegistry.CallOpts, _names)
}

// UpdateContractAddresses is a paid mutator transaction binding the contract method 0xb00c0b76.
//
// Solidity: function updateContractAddresses(bytes32[] _contractNameHashes, address[] _contractAddresses) returns()
func (_ContractRegistry *ContractRegistryTransactor) UpdateContractAddresses(opts *bind.TransactOpts, _contractNameHashes [][32]byte, _contractAddresses []common.Address) (*types.Transaction, error) {
	return _ContractRegistry.contract.Transact(opts, "updateContractAddresses", _contractNameHashes, _contractAddresses)
}

// UpdateContractAddresses is a paid mutator transaction binding the contract method 0xb00c0b76.
//
// Solidity: function updateContractAddresses(bytes32[] _contractNameHashes, address[] _contractAddresses) returns()
func (_ContractRegistry *ContractRegistrySession) UpdateContractAddresses(_contractNameHashes [][32]byte, _contractAddresses []common.Address) (*types.Transaction, error) {
	return _ContractRegistry.Contract.UpdateContractAddresses(&_ContractRegistry.TransactOpts, _contractNameHashes, _contractAddresses)
}

// UpdateContractAddresses is a paid mutator transaction binding the contract method 0xb00c0b76.
//
// Solidity: function updateContractAddresses(bytes32[] _contractNameHashes, address[] _contractAddresses) returns()
func (_ContractRegistry *ContractRegistryTransactorSession) UpdateContractAddresses(_contractNameHashes [][32]byte, _contractAddresses []common.Address) (*types.Transaction, error) {
	return _ContractRegistry.Contract.UpdateContractAddresses(&_ContractRegistry.TransactOpts, _contractNameHashes, _contractAddresses)
}
