package handlers

import (
	"encoding/hex"
	"fmt"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/asolovov/evm-oracle-demo-api/internal/models"
)

// requestPriceArgs is the ABI argument tuple for PriceAggregator.requestPrice.
// We pin a single bytes32 argument because that's what the deployed contract
// surfaces — the contract repo's ABI is fixed at deploy time.
var requestPriceArgs = func() abi.Arguments {
	bytes32, err := abi.NewType("bytes32", "", nil)
	if err != nil {
		// abi.NewType("bytes32") never errors in go-ethereum v1.14.x.
		panic(fmt.Sprintf("init bytes32 arg type: %v", err))
	}
	return abi.Arguments{{Type: bytes32}}
}()

// requestPriceSelector caches the 4-byte function selector for
// `requestPrice(bytes32)` so we don't recompute the keccak hash per request.
var requestPriceSelector = func() []byte {
	sum := crypto.Keccak256([]byte("requestPrice(bytes32)"))
	return sum[:4]
}()

// encodeRequestPriceCalldata returns the 0x-prefixed lowercase hex
// concatenation of the function selector and ABI-encoded asset id bytes32.
// The on-chain asset id is keccak256(symbol); callers pass the catalog id
// ("weth") and we resolve it to the same bytes32 the contract expects.
func encodeRequestPriceCalldata(assetID string) (string, error) {
	bytes32Hex, ok := models.AssetIDHash(assetID)
	if !ok {
		return "", fmt.Errorf("asset id %q is not a tracked asset", assetID)
	}
	raw, err := hex.DecodeString(bytes32Hex[2:])
	if err != nil || len(raw) != 32 {
		return "", fmt.Errorf("asset id %q does not encode to a valid bytes32", assetID)
	}
	var fixed [32]byte
	copy(fixed[:], raw)

	encoded, err := requestPriceArgs.Pack(fixed)
	if err != nil {
		return "", fmt.Errorf("abi.Pack(requestPrice, %s): %w", assetID, err)
	}
	out := append(append([]byte{}, requestPriceSelector...), encoded...)
	return "0x" + hex.EncodeToString(out), nil
}
