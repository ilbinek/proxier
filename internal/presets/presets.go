// Package presets defines the built-in rule templates.
package presets

import "proxier/internal/model"

const ts3Hint = "All proxied players reach the TeamSpeak server from this VPS's IP. " +
	"Ask the TS3 server admin to add that IP to query_ip_allowlist.txt (exempt from anti-flood), " +
	"otherwise one misbehaving client can get the proxy, and everyone behind it, temporarily banned. " +
	"Keep the file transfer port identical on both sides: the server tells clients which port to use."

var builtins = []*model.Preset{
	{
		ID: "builtin:ts3", Name: "TeamSpeak 3", Builtin: true,
		Description: "Voice (UDP 9987) and file transfer (TCP 30033). ServerQuery optional.",
		Hint:        ts3Hint,
		Rules: []model.PresetRule{
			{Name: "Voice", Protocol: model.ProtoUDP, PortStart: 9987, PortEnd: 9987},
			{Name: "File transfer", Protocol: model.ProtoTCP, PortStart: 30033, PortEnd: 30033, SamePort: true},
			{Name: "ServerQuery (raw)", Protocol: model.ProtoTCP, PortStart: 10011, PortEnd: 10011, Optional: true},
			{Name: "ServerQuery (SSH)", Protocol: model.ProtoTCP, PortStart: 10022, PortEnd: 10022, Optional: true},
		},
	},
	{
		ID: "builtin:minecraft-java", Name: "Minecraft: Java Edition", Builtin: true,
		Description: "Game port TCP 25565.",
		Rules: []model.PresetRule{
			{Name: "Game", Protocol: model.ProtoTCP, PortStart: 25565, PortEnd: 25565},
			{Name: "Query", Protocol: model.ProtoUDP, PortStart: 25565, PortEnd: 25565, Optional: true},
			{Name: "RCON", Protocol: model.ProtoTCP, PortStart: 25575, PortEnd: 25575, Optional: true},
		},
	},
	{
		ID: "builtin:minecraft-bedrock", Name: "Minecraft: Bedrock Edition", Builtin: true,
		Description: "Game port UDP 19132 (IPv6 port 19133 optional).",
		Rules: []model.PresetRule{
			{Name: "Game (IPv4)", Protocol: model.ProtoUDP, PortStart: 19132, PortEnd: 19132},
			{Name: "Game (IPv6)", Protocol: model.ProtoUDP, PortStart: 19133, PortEnd: 19133, Optional: true},
		},
	},
	{
		ID: "builtin:source", Name: "CS2 / Source engine", Builtin: true,
		Description: "Game UDP 27015 and RCON TCP 27015. SourceTV optional.",
		Rules: []model.PresetRule{
			{Name: "Game + RCON", Protocol: model.ProtoBoth, PortStart: 27015, PortEnd: 27015},
			{Name: "SourceTV", Protocol: model.ProtoUDP, PortStart: 27020, PortEnd: 27020, Optional: true},
		},
	},
	{
		ID: "builtin:rust", Name: "Rust", Builtin: true,
		Description: "Game UDP 28015. RCON and Rust+ companion optional.",
		Rules: []model.PresetRule{
			{Name: "Game", Protocol: model.ProtoUDP, PortStart: 28015, PortEnd: 28015},
			{Name: "Query", Protocol: model.ProtoUDP, PortStart: 28017, PortEnd: 28017, Optional: true},
			{Name: "RCON", Protocol: model.ProtoTCP, PortStart: 28016, PortEnd: 28016, Optional: true},
			{Name: "Rust+ app", Protocol: model.ProtoTCP, PortStart: 28082, PortEnd: 28082, Optional: true},
		},
	},
	{
		ID: "builtin:valheim", Name: "Valheim", Builtin: true,
		Description: "Game and query UDP 2456-2458.",
		Rules: []model.PresetRule{
			{Name: "Game", Protocol: model.ProtoUDP, PortStart: 2456, PortEnd: 2458},
		},
	},
	{
		ID: "builtin:factorio", Name: "Factorio", Builtin: true,
		Description: "Game UDP 34197.",
		Rules: []model.PresetRule{
			{Name: "Game", Protocol: model.ProtoUDP, PortStart: 34197, PortEnd: 34197},
		},
	},
	{
		ID: "builtin:terraria", Name: "Terraria", Builtin: true,
		Description: "Game TCP 7777.",
		Rules: []model.PresetRule{
			{Name: "Game", Protocol: model.ProtoTCP, PortStart: 7777, PortEnd: 7777},
		},
	},
	{
		ID: "builtin:wireguard", Name: "WireGuard", Builtin: true,
		Description: "VPN tunnel UDP 51820.",
		Rules: []model.PresetRule{
			{Name: "Tunnel", Protocol: model.ProtoUDP, PortStart: 51820, PortEnd: 51820, UDPTimeout: 300},
		},
	},
}

// Builtins returns the built-in presets.
func Builtins() []*model.Preset { return builtins }

// Builtin looks up a built-in preset by ID.
func Builtin(id string) *model.Preset {
	for _, p := range builtins {
		if p.ID == id {
			return p
		}
	}
	return nil
}
