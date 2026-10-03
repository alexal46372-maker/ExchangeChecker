package ipc

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_IPC_06_ErrorReplyProfileID — IPCError.ProfileID reaches the
// error reply as "profileId" (omitted when empty); code, errorKey,
// errorArgs and the text are unchanged.
func TestCHK_IPC_06_ErrorReplyProfileID(t *testing.T) {
	ie := &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgMonitorProfileInvalid,
		Message: `profile "bad": server "host\\": empty instance name`, Args: []string{"server", model.ValServerFormat}, ProfileID: "bad"}
	m := errorReply(7, ie, "")
	if m.ProfileID != "bad" || m.Code != ie.Code || m.ErrorKey != ie.Key || m.Error != ie.Message || len(m.ErrorArgs) != 2 || m.ErrorArgs[0] != "server" {
		t.Fatalf("%+v", m)
	}
	b, _ := json.Marshal(m)
	if !strings.Contains(string(b), `"profileId":"bad"`) {
		t.Fatalf("JSON: %s", b)
	}
	ie.ProfileID = ""
	b, _ = json.Marshal(errorReply(8, ie, ""))
	if strings.Contains(string(b), "profileId") {
		t.Fatalf("empty profileId not omitted: %s", b)
	}
}
