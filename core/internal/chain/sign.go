package chain

// sign.go is a faithful Go-native port of poker-vm/bot/go/cosmos.go: it derives
// a b52 key, hand-encodes the Cosmos tx envelope (SIGN_MODE_DIRECT, gasless),
// signs with secp256k1, and broadcasts in SYNC mode. The wire encoding (field
// tags, Any wrapping, SignDoc) MUST match the chain byte-for-byte — a single
// wrong tag produces a silent signature-verification failure. Keep it aligned
// with the bot reference.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/cosmos/go-bip39"
)

const (
	hdPath        = "m/44'/118'/0'/0/0" // Cosmos coin type 118
	addressPrefix = "b52"
	gasLimit      = 1_000_000
)

// wallet holds the derived signing identity. Constructed from a mnemonic or a
// raw private key — "own money" is just the agent's own wallet.
type wallet struct {
	priv cryptotypes.PrivKey
	pub  cryptotypes.PubKey
	addr sdk.AccAddress
}

func init() {
	// Register the b52 bech32 prefix process-wide (sdk config is a singleton).
	cfg := sdk.GetConfig()
	cfg.SetBech32PrefixForAccount(addressPrefix, addressPrefix+"pub")
}

func walletFromMnemonic(mnemonic string) (*wallet, error) {
	if !bip39.IsMnemonicValid(mnemonic) {
		return nil, fmt.Errorf("invalid mnemonic")
	}
	seed, err := bip39.NewSeedWithErrorChecking(mnemonic, "")
	if err != nil {
		return nil, fmt.Errorf("seed: %w", err)
	}
	master, ch := hd.ComputeMastersFromSeed(seed)
	derived, err := hd.DerivePrivateKeyForPath(master, ch, hdPath)
	if err != nil {
		return nil, fmt.Errorf("derive: %w", err)
	}
	return newWallet(&secp256k1.PrivKey{Key: derived}), nil
}

func walletFromPrivateKeyHex(h string) (*wallet, error) {
	keyBytes, err := hex.DecodeString(h)
	if err != nil {
		return nil, fmt.Errorf("decode private key: %w", err)
	}
	if len(keyBytes) != 32 {
		return nil, fmt.Errorf("private key must be 32 bytes, got %d", len(keyBytes))
	}
	return newWallet(&secp256k1.PrivKey{Key: keyBytes}), nil
}

func newWallet(priv *secp256k1.PrivKey) *wallet {
	pub := priv.PubKey()
	return &wallet{priv: priv, pub: pub, addr: sdk.AccAddress(pub.Address())}
}

func (w *wallet) address() string { return w.addr.String() }

// --- account query ---

type accountInfo struct {
	accountNumber uint64
	sequence      uint64
}

func (c *Client) accountInfo(ctx context.Context, addr string) (*accountInfo, error) {
	var result struct {
		Account struct {
			AccountNumber string `json:"account_number"`
			Sequence      string `json:"sequence"`
		} `json:"account"`
	}
	if err := c.getJSON(ctx, "/cosmos/auth/v1beta1/accounts/"+addr, &result); err != nil {
		return nil, err
	}
	accNum, _ := strconv.ParseUint(result.Account.AccountNumber, 10, 64)
	seq, _ := strconv.ParseUint(result.Account.Sequence, 10, 64)
	return &accountInfo{accountNumber: accNum, sequence: seq}, nil
}

// --- sign + broadcast (SYNC) ---

// signAndBroadcast assembles the tx envelope for a single message, signs it in
// SIGN_MODE_DIRECT, and broadcasts SYNC. Returns the tx hash.
func (c *Client) signAndBroadcast(ctx context.Context, w *wallet, typeURL string, msgBytes []byte, memo string) (string, error) {
	acc, err := c.accountInfo(ctx, w.address())
	if err != nil {
		return "", fmt.Errorf("account info: %w", err)
	}
	anyMsg := encodeAny(typeURL, msgBytes)
	body := encodeTxBody([][]byte{anyMsg}, memo)
	authInfo := encodeAuthInfo(w.pub.Bytes(), acc.sequence, gasLimit)
	signDoc := encodeSignDoc(body, authInfo, c.cfg.ChainID, acc.accountNumber)

	sig, err := w.priv.Sign(signDoc)
	if err != nil {
		return "", fmt.Errorf("sign: %w", err)
	}
	txRaw := encodeTxRaw(body, authInfo, [][]byte{sig})
	return c.broadcastSync(ctx, txRaw)
}

func (c *Client) broadcastSync(ctx context.Context, txBytes []byte) (string, error) {
	reqBody, _ := json.Marshal(map[string]any{
		"tx_bytes": base64.StdEncoding.EncodeToString(txBytes),
		"mode":     "BROADCAST_MODE_SYNC",
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.RestEndpoint+"/cosmos/tx/v1beta1/txs", bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("broadcast: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	var result struct {
		TxResponse struct {
			TxHash string `json:"txhash"`
			Code   int    `json:"code"`
			RawLog string `json:"raw_log"`
		} `json:"tx_response"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("parse broadcast response: %w (body: %s)", err, string(raw))
	}
	if result.TxResponse.Code != 0 {
		return "", fmt.Errorf("tx failed code %d: %s", result.TxResponse.Code, result.TxResponse.RawLog)
	}
	return result.TxResponse.TxHash, nil
}

// --- message encoders (field tags mirror pokerchain tx.proto) ---

func encodeMsgPerformAction(player, gameID, action string, amount uint64, data string) []byte {
	var buf bytes.Buffer
	writeStringField(&buf, 1, player)
	writeStringField(&buf, 2, gameID)
	writeStringField(&buf, 3, action)
	writeVarintField(&buf, 4, amount)
	writeStringField(&buf, 5, data)
	return buf.Bytes()
}

func encodeMsgJoinGame(player, gameID string, seat, buyIn uint64) []byte {
	var buf bytes.Buffer
	writeStringField(&buf, 1, player)
	writeStringField(&buf, 2, gameID)
	writeVarintField(&buf, 3, seat)
	writeVarintField(&buf, 4, buyIn)
	return buf.Bytes()
}

func encodeMsgLeaveGame(creator, gameID string) []byte {
	var buf bytes.Buffer
	writeStringField(&buf, 1, creator)
	writeStringField(&buf, 2, gameID)
	return buf.Bytes()
}

// --- tx envelope encoders (Cosmos tx.proto) ---

func encodeAny(typeURL string, value []byte) []byte {
	var buf bytes.Buffer
	writeStringField(&buf, 1, typeURL)
	writeBytesField(&buf, 2, value)
	return buf.Bytes()
}

func encodeTxBody(messages [][]byte, memo string) []byte {
	var buf bytes.Buffer
	for _, msg := range messages {
		writeBytesField(&buf, 1, msg)
	}
	writeStringField(&buf, 2, memo)
	return buf.Bytes()
}

func encodeAuthInfo(pubKeyBytes []byte, sequence, gas uint64) []byte {
	var buf bytes.Buffer
	writeBytesField(&buf, 1, encodeSignerInfo(pubKeyBytes, sequence))
	writeBytesField(&buf, 2, encodeFee(gas))
	return buf.Bytes()
}

func encodeSignerInfo(pubKeyBytes []byte, sequence uint64) []byte {
	var buf bytes.Buffer
	writeBytesField(&buf, 1, encodePubKeyAny(pubKeyBytes))
	writeBytesField(&buf, 2, encodeModeInfo())
	writeVarintField(&buf, 3, sequence)
	return buf.Bytes()
}

func encodePubKeyAny(pubKeyBytes []byte) []byte {
	var keyBuf bytes.Buffer
	writeBytesField(&keyBuf, 1, pubKeyBytes)
	return encodeAny("/cosmos.crypto.secp256k1.PubKey", keyBuf.Bytes())
}

func encodeModeInfo() []byte {
	var buf bytes.Buffer
	writeBytesField(&buf, 1, encodeSingle())
	return buf.Bytes()
}

func encodeSingle() []byte {
	var buf bytes.Buffer
	writeVarintField(&buf, 1, uint64(signing.SignMode_SIGN_MODE_DIRECT))
	return buf.Bytes()
}

func encodeFee(gas uint64) []byte {
	var buf bytes.Buffer
	// Field 1 (amount, repeated Coin) omitted — gasless. Field 2: gas_limit.
	writeVarintField(&buf, 2, gas)
	return buf.Bytes()
}

func encodeSignDoc(bodyBytes, authInfoBytes []byte, chainID string, accountNumber uint64) []byte {
	var buf bytes.Buffer
	writeBytesField(&buf, 1, bodyBytes)
	writeBytesField(&buf, 2, authInfoBytes)
	writeStringField(&buf, 3, chainID)
	writeVarintField(&buf, 4, accountNumber)
	return buf.Bytes()
}

func encodeTxRaw(bodyBytes, authInfoBytes []byte, signatures [][]byte) []byte {
	var buf bytes.Buffer
	writeBytesField(&buf, 1, bodyBytes)
	writeBytesField(&buf, 2, authInfoBytes)
	for _, sig := range signatures {
		writeBytesField(&buf, 3, sig)
	}
	return buf.Bytes()
}

// --- protobuf wire primitives ---

// fieldTag is (fieldNum << 3) | wireType. wireType 2 = length-delimited
// (string/bytes/embedded), 0 = varint.
func writeStringField(buf *bytes.Buffer, field int, s string) {
	if s == "" {
		return
	}
	buf.WriteByte(byte(field<<3) | 2)
	writeVarint(buf, uint64(len(s)))
	buf.WriteString(s)
}

func writeBytesField(buf *bytes.Buffer, field int, b []byte) {
	buf.WriteByte(byte(field<<3) | 2)
	writeVarint(buf, uint64(len(b)))
	buf.Write(b)
}

func writeVarintField(buf *bytes.Buffer, field int, v uint64) {
	if v == 0 {
		return
	}
	buf.WriteByte(byte(field << 3)) // wire type 0
	writeVarint(buf, v)
}

func writeVarint(buf *bytes.Buffer, v uint64) {
	for v >= 0x80 {
		buf.WriteByte(byte(v&0x7f) | 0x80)
		v >>= 7
	}
	buf.WriteByte(byte(v))
}
