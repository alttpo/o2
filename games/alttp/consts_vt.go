package alttp

var (
	vtItemBitNames = map[uint16][8]string{
		0x38C: {
			"Flute (active)",
			"Flute (inactive)",
			"Shovel",
			"",
			"Magic Powder",
			"Mushroom",
			"Red Boomerang",
			"Blue Boomerang",
		},
		0x38E: {
			"",
			"",
			"",
			"",
			"",
			"", // 2nd Progressive Bow
			"Silver Bow",
			"Bow",
		},
	}

	vtItemBitVerbs = map[uint16][8]string{}
)
