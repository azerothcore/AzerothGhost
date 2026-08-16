module github.com/azerothcore/AzerothGhost

go 1.26.0

replace github.com/o0olele/detour-go => github.com/walkline/detour-go v0.0.0-20260624174442-8056537019f8

require github.com/o0olele/detour-go v0.0.0-20260624174442-8056537019f8

require (
	github.com/Shopify/go-lua v0.0.0-20250718183320-1e37f32ad7d0
	github.com/go-sql-driver/mysql v1.10.0
)

require filippo.io/edwards25519 v1.2.0 // indirect
