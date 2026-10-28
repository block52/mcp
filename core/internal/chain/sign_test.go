package chain

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

// A fixed, well-known BIP39 test mnemonic (NOT a real wallet).
const testMnemonic = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

func TestWalletFromMnemonicDerivesB52Address(t *testing.T) {
	w, err := walletFromMnemonic(testMnemonic)
	if err != nil {
		t.Fatalf("walletFromMnemonic: %v", err)
	}
	addr := w.address()
	if !strings.HasPrefix(addr, addressPrefix) {
		t.Fatalf("address %q lacks %q prefix", addr, addressPrefix)
	}
	// Derivation must be deterministic.
	w2, _ := walletFromMnemonic(testMnemonic)
	if w2.address() != addr {
		t.Fatalf("derivation not deterministic: %q vs %q", addr, w2.address())
	}
}

func TestWalletFromPrivateKeyValidation(t *testing.T) {
	if _, err := walletFromPrivateKeyHex("zz"); err == nil {
		t.Fatal("expected error for non-hex key")
	}
	if _, err := walletFromPrivateKeyHex("abcd"); err == nil {
		t.Fatal("expected error for short key")
	}
	// 32 valid bytes should work.
	full := strings.Repeat("01", 32)
	if _, err := walletFromPrivateKeyHex(full); err != nil {
		t.Fatalf("valid 32-byte key rejected: %v", err)
	}
}

func TestInvalidMnemonicRejected(t *testing.T) {
	if _, err := walletFromMnemonic("not a real mnemonic phrase at all"); err == nil {
		t.Fatal("expected invalid mnemonic error")
	}
}

// decodeFields walks a protobuf message and returns field number -> raw value.
// Strings/bytes come back as []byte; varints as uint64. Verifies our hand-rolled
// encoders emit bytes the canonical protowire decoder accepts with the right tags.
func decodeFields(t *testing.T, b []byte) map[protowire.Number]any {
	t.Helper()
	out := map[protowire.Number]any{}
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			t.Fatalf("bad tag")
		}
		b = b[n:]
		switch typ {
		case protowire.VarintType:
			v, m := protowire.ConsumeVarint(b)
			if m < 0 {
				t.Fatalf("bad varint")
			}
			out[num] = v
			b = b[m:]
		case protowire.BytesType:
			v, m := protowire.ConsumeBytes(b)
			if m < 0 {
				t.Fatalf("bad bytes")
			}
			cp := make([]byte, len(v))
			copy(cp, v)
			out[num] = cp
			b = b[m:]
		default:
			t.Fatalf("unexpected wire type %d for field %d", typ, num)
		}
	}
	return out
}

func TestEncodeMsgPerformActionWireFormat(t *testing.T) {
	b := encodeMsgPerformAction("b52player", "b52game", "raise", 50_000_000, "cards")
	f := decodeFields(t, b)

	if got := string(f[1].([]byte)); got != "b52player" {
		t.Fatalf("field1 player = %q", got)
	}
	if got := string(f[2].([]byte)); got != "b52game" {
		t.Fatalf("field2 gameId = %q", got)
	}
	if got := string(f[3].([]byte)); got != "raise" {
		t.Fatalf("field3 action = %q", got)
	}
	if got := f[4].(uint64); got != 50_000_000 {
		t.Fatalf("field4 amount = %d", got)
	}
	if got := string(f[5].([]byte)); got != "cards" {
		t.Fatalf("field5 data = %q", got)
	}
}

func TestEncodeMsgPerformActionOmitsZeroAmount(t *testing.T) {
	// fold: amount 0, no data — fields 4 and 5 must be absent (proto3 default).
	b := encodeMsgPerformAction("b52player", "b52game", "fold", 0, "")
	f := decodeFields(t, b)
	if _, ok := f[4]; ok {
		t.Fatal("zero amount should be omitted")
	}
	if _, ok := f[5]; ok {
		t.Fatal("empty data should be omitted")
	}
}

func TestEncodeMsgJoinGameWireFormat(t *testing.T) {
	b := encodeMsgJoinGame("b52player", "b52game", 3, 100_000_000)
	f := decodeFields(t, b)
	if got := string(f[1].([]byte)); got != "b52player" {
		t.Fatalf("field1 = %q", got)
	}
	if got := f[3].(uint64); got != 3 {
		t.Fatalf("field3 seat = %d", got)
	}
	if got := f[4].(uint64); got != 100_000_000 {
		t.Fatalf("field4 buyIn = %d", got)
	}
}

func TestEncodeAnyWrapsTypeURL(t *testing.T) {
	inner := encodeMsgLeaveGame("b52player", "b52game")
	any := encodeAny("/pokerchain.poker.v1.MsgLeaveGame", inner)
	f := decodeFields(t, any)
	if got := string(f[1].([]byte)); got != "/pokerchain.poker.v1.MsgLeaveGame" {
		t.Fatalf("Any.type_url = %q", got)
	}
	// Any.value (field 2) must be the inner message bytes verbatim.
	if got := f[2].([]byte); string(got) != string(inner) {
		t.Fatalf("Any.value mismatch")
	}
}

func TestEncodeSignDocStructure(t *testing.T) {
	body := encodeTxBody([][]byte{encodeAny("/x", []byte{1, 2})}, "memo")
	auth := encodeAuthInfo([]byte("pubkeybytes"), 7, gasLimit)
	doc := encodeSignDoc(body, auth, "pokerchain", 42)
	f := decodeFields(t, doc)
	if string(f[1].([]byte)) != string(body) {
		t.Fatal("signDoc field1 != body")
	}
	if string(f[2].([]byte)) != string(auth) {
		t.Fatal("signDoc field2 != authInfo")
	}
	if string(f[3].([]byte)) != "pokerchain" {
		t.Fatal("signDoc field3 != chainID")
	}
	if f[4].(uint64) != 42 {
		t.Fatal("signDoc field4 != accountNumber")
	}
}
