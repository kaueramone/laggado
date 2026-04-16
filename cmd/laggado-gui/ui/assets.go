package ui

import _ "embed"

// pixQRBytes is the real PIX static QR Code for kaueramone@live.com (Kaue Da Costa Pacheco).
// Generated from EMV payload with CRC16/CCITT-FALSE (checksum 0BE6).
// Payload: 00020126410014BR.GOV.BCB.PIX0119kaueramone@live.com...63040BE6
//
//go:embed assets/pix_qr.png
var pixQRBytes []byte
