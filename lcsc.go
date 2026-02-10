// Package lcsc provides an unofficial Go client for LCSC component data.
//
// Endpoints are organized into service groups:
//
//   - client.Search  - keyword search
//   - client.Product - product details
//
// LCSC does not provide an official public API for this data. This package
// uses undocumented endpoints discovered from the web application and they can
// change without notice.
package lcsc

// Version is the current version of the go-lcsc package.
const Version = "1.0.0"
