// Package lcsc provides an unofficial Go client for LCSC component data.
//
// Endpoints are organized into service groups:
//
//   - client.Search     - keyword, parametric and filter search
//   - client.Product    - product details and datasheet URLs
//   - client.Alternates - cross-reference alternates
//   - client.Catalog    - category tree
//   - client.ThirdParty - marketplace offers of third-party suppliers
//
// [ImageURLAtSize] changes the size of an LCSC product image URL without a
// request.
//
// LCSC does not provide an official public API for this data. This package
// uses undocumented endpoints discovered from the web application and they can
// change without notice.
package lcsc

// Version is the current version of the go-lcsc package.
const Version = "1.2.0"
