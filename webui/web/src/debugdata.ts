// What the debug page can give and take away, and the game's HUD tile map words that draw each one.
//
// A word is a BG3 tile map entry, as the game's HUD code (hud.c in the zelda3 reimplementation, bank $0D) builds the
// item menu: bits 0-9 tile number, 10-12 palette, 14 horizontal flip, 15 vertical flip. An icon is 2x2 tiles
// (top left, top right, bottom left, bottom right). The tiles themselves and the palette come from the ROM (see
// debuggfx.tsx); nothing here is a picture.

import {DebugOp} from "./debuggfx";

// the SRAM copy in WRAM is $7EF000..$7EFFFF; the page sees $F340..$F3FF:
export const SRAM_FIRST = 0xF340;

// CGRAM index of the gold of the item box frame (palette 2, color 1):
export const HUD_GOLD = 9;

export const EMPTY_ICON = [0x20f5, 0x20f5, 0x20f5, 0x20f5];

export interface Choice {
    name: string;
    words: number[];
    // the tiles per row of words, 2 when omitted:
    w?: number;
    // whether the player has it, given the SRAM bytes:
    has: (get: Getter) => boolean;
    // CGRAM colors not to draw:
    hide?: number[];
    // the operations that give it, and take it away:
    give: DebugOp[];
    take: DebugOp[];
}

export type Getter = (addr: number) => number;

const set = (a: number, v: number): DebugOp => ({t: "set", a, v});
const or = (a: number, m: number): DebugOp => ({t: "or", a, m});
const and = (a: number, m: number): DebugOp => ({t: "and", a, m});

// item has the player's value of the byte at addr be the level, or any value from it up when `atLeast`:
function item(name: string, addr: number, value: number, words: number[], takeValue = 0): Choice {
    return {
        name, words,
        has: get => get(addr) === value,
        give: [set(addr, value)],
        take: [set(addr, takeValue)],
    };
}

// ---- Y items, laid out as the item menu does: 5 columns, 4 rows ($7EF340..$7EF353).
// A click cycles a cell through its levels: none, then each level, then none again.

export interface Level {
    name: string;
    words: number[];
    // for a level that has both items of a slot: the second item, drawn with the first one on the other side of a diagonal:
    words2?: number[];
    // the operations that make the player have this level:
    ops: DebugOp[];
}

export interface Cell {
    name: string;
    levels: Level[];
    // the index in levels the player is at:
    current: (get: Getter) => number;
}

const NONE = (name: string, addr: number): Level => ({name: "No " + name, words: EMPTY_ICON, ops: [set(addr, 0)]});

// cell makes a cell whose byte at addr is 0 for none, or one of the values, each with its words; the current level is
// where `level` (the byte's value by default) is found:
function cell(name: string, addr: number, levels: [string, number, number[]][], level?: (v: number) => number): Cell {
    const values = [0, ...levels.map(l => l[1])];
    return {
        name,
        levels: [NONE(name, addr), ...levels.map(([n, v, words]) => ({name: n, words, ops: [set(addr, v)]}))],
        current: get => {
            const v = get(addr);
            return level ? level(v) : Math.max(0, values.indexOf(v));
        },
    };
}

// ---- items that share a slot of the item menu (the Y button swaps them). Each has a byte for the one equipped, and the
// randomizer keeps what the player has in a bitfield ($7EF38C, bit 7 blue boomerang, 6 red boomerang, 5 mushroom, 4
// magic powder, 3 mushroom from the past, 2 shovel, 1 inactive flute, 0 active flute); without the bits (the original
// game) the byte is all there is.

export const TRACKING = 0xF38C;

interface Side {
    name: string;
    // the value of the equipped byte:
    value: number;
    words: number[];
    // the bits to set when the player has it, and the bits that tell that they do:
    set: number;
    seen: number;
}

// pair makes a cell of two items that can be had alone or both: none, the first, the second, both.
function pair(name: string, addr: number, a: Side, b: Side, byteB = (v: number) => v === b.value): Cell {
    const all = a.set | b.set | a.seen | b.seen;
    const ops = (value: number, bits: number): DebugOp[] => [set(addr, value), and(TRACKING, ~all & 0xFF), or(TRACKING, bits)];
    return {
        name,
        levels: [
            {name: "No " + name, words: EMPTY_ICON, ops: ops(0, 0)},
            {name: a.name, words: a.words, ops: ops(a.value, a.set)},
            {name: b.name, words: b.words, ops: ops(b.value, b.set)},
            {name: a.name + " and " + b.name, words: a.words, words2: b.words, ops: ops(a.value, a.set | b.set)},
        ],
        current: get => {
            const bits = get(TRACKING);
            const hasA = (bits & a.seen) !== 0;
            const hasB = (bits & b.seen) !== 0;
            if (hasA && hasB) return 3;
            if (hasA) return 1;
            if (hasB) return 2;
            const v = get(addr);
            return v === a.value ? 1 : byteB(v) ? 2 : 0;
        },
    };
}

const simple = (name: string, addr: number, words: number[]) => cell(name, addr, [[name, 1, words]], v => v !== 0 ? 1 : 0);

export const BOTTLE_CELL = "Bottles";

export const yGrid: Cell[] = [
    cell("Bow", 0xF340, [
        ["Bow", 2, [0x28ba, 0x284a, 0x2849, 0x28cb]],
        ["Silver Bow", 4, [0x28ba, 0x28bb, 0x24ca, 0x28cb]],
    ], v => v === 0 ? 0 : v <= 2 ? 1 : 2),
    pair("Boomerangs", 0xF341,
        {name: "Blue Boomerang", value: 1, words: [0x2cb8, 0x2cb9, 0x2cf5, 0x2cc9], set: 0x80, seen: 0x80},
        {name: "Red Boomerang", value: 2, words: [0x24b8, 0x24b9, 0x24f5, 0x24c9], set: 0x40, seen: 0x40}),
    simple("Hookshot", 0xF342, [0x24f5, 0x24f6, 0x24c0, 0x24f5]),
    // $7EF343 is the number of bombs:
    cell("Bombs", 0xF343, [["Bombs (10)", 10, [0x2cb2, 0x2cb3, 0x2cc2, 0x6cc2]]], v => v > 0 ? 1 : 0),
    pair("Mushroom and Magic Powder", 0xF344,
        {name: "Mushroom", value: 1, words: [0x2444, 0x2445, 0x2446, 0x2447], set: 0x28, seen: 0x20},
        {name: "Magic Powder", value: 2, words: [0x203b, 0x203c, 0x203d, 0x203e], set: 0x10, seen: 0x10}),

    simple("Fire Rod", 0xF345, [0x24b0, 0x24b1, 0x24c0, 0x24c1]),
    simple("Ice Rod", 0xF346, [0x2cb0, 0x2cbe, 0x2cc0, 0x2cc1]),
    simple("Bombos", 0xF347, [0x287d, 0x287e, 0xe87e, 0xe87d]),
    simple("Ether", 0xF348, [0x2876, 0x2877, 0xe877, 0xe876]),
    simple("Quake", 0xF349, [0x2866, 0x2867, 0xe867, 0xe866]),

    simple("Lamp", 0xF34A, [0x24bc, 0x24bd, 0x24cc, 0x24cd]),
    simple("Magic Hammer", 0xF34B, [0x20b6, 0x20b7, 0x20c6, 0x20c7]),
    // the flute is $02 until it is activated, then $03:
    pair("Shovel and Flute", 0xF34C,
        {name: "Shovel", value: 1, words: [0x20d0, 0x20d1, 0x20e0, 0x20e1], set: 0x04, seen: 0x04},
        {name: "Flute", value: 3, words: [0x2cd4, 0x2cd5, 0x2ce4, 0x2ce5], set: 0x01, seen: 0x03}, v => v >= 2),
    simple("Bug-Catching Net", 0xF34D, [0x3c40, 0x3c41, 0x2842, 0x3c43]),
    simple("Book of Mudora", 0xF34E, [0x3ca5, 0x3ca6, 0x3cd8, 0x3cd9]),

    // the bottle cell is handled by the bottle logic:
    {name: BOTTLE_CELL, levels: [], current: () => 0},
    simple("Cane of Somaria", 0xF350, [0x24dc, 0x24dd, 0x24ec, 0x24ed]),
    simple("Cane of Byrna", 0xF351, [0x2cdc, 0x2cdd, 0x2cec, 0x2ced]),
    simple("Magic Cape", 0xF352, [0x24b4, 0x24b5, 0x24c4, 0x24c5]),
    cell("Magic Mirror", 0xF353, [["Magic Mirror", 2, [0x2c62, 0x2c63, 0x2c72, 0x2c73]]], v => v !== 0 ? 1 : 0),
];

// ---- bottles ($7EF35C..$7EF35F hold what each of the 4 bottles contains, 0 for no bottle; $7EF34F is the bottle
// selected, 1..4):

export const BOTTLE_ADDR = 0xF35C;
export const BOTTLE_COUNT = 4;
export const BOTTLE_SELECTED = 0xF34F;

// bottleContents[n] is what a bottle holding n shows in the item menu:
export const bottleContents: { name: string, words: number[] }[] = [
    {name: "No bottle", words: EMPTY_ICON},
    {name: "Mushroom", words: [0x2044, 0x2045, 0x2046, 0x2047]},
    {name: "Empty Bottle", words: [0x2837, 0x2838, 0x2cc3, 0x2cd3]},
    {name: "Red Potion", words: [0x24d2, 0x64d2, 0x24e2, 0x24e3]},
    {name: "Green Potion", words: [0x3cd2, 0x7cd2, 0x3ce2, 0x3ce3]},
    {name: "Blue Potion", words: [0x2cd2, 0x6cd2, 0x2ce2, 0x2ce3]},
    {name: "Fairy", words: [0x2855, 0x6855, 0x2c57, 0x2c5a]},
    {name: "Bee", words: [0x2837, 0x2838, 0x2839, 0x283a]},
    {name: "Good Bee", words: [0x2837, 0x2838, 0x2839, 0x283a]},
];

// the order a click cycles a bottle's contents in:
export const bottleCycle = [2, 3, 4, 5, 6, 7, 8, 1];

// ---- equipment, with a level per icon:

export interface Equipment {
    name: string;
    addr: number;
    // the refresh of Link's graphics the game does after a change:
    gfx: "sword" | "shield" | "armor";
    levels: { name: string, words: number[] }[];
}

export const equipment: Equipment[] = [
    {
        name: "Sword", addr: 0xF359, gfx: "sword", levels: [
            {name: "No sword", words: EMPTY_ICON},
            {name: "Fighter's Sword", words: [0x2c64, 0x2cce, 0x2c75, 0x3d25]},
            {name: "Master Sword", words: [0x2c8a, 0x2c65, 0x2474, 0x3d26]},
            {name: "Tempered Sword", words: [0x248a, 0x2465, 0x3c74, 0x2d48]},
            {name: "Golden Sword", words: [0x288a, 0x2865, 0x2c74, 0x2d39]},
        ]
    },
    {
        name: "Shield", addr: 0xF35A, gfx: "shield", levels: [
            {name: "No shield", words: [0x24f5, 0x24f5, 0x24f5, 0x24f5]},
            {name: "Fighter's Shield", words: [0x2cfd, 0x6cfd, 0x2cfe, 0x6cfe]},
            {name: "Fire Shield", words: [0x34ff, 0x74ff, 0x349f, 0x749f]},
            {name: "Mirror Shield", words: [0x2880, 0x2881, 0x288d, 0x288e]},
        ]
    },
    {
        name: "Armor", addr: 0xF35B, gfx: "armor", levels: [
            {name: "Green Mail", words: [0x3c68, 0x7c68, 0x3c78, 0x7c78]},
            {name: "Blue Mail", words: [0x2c68, 0x6c68, 0x2c78, 0x6c78]},
            {name: "Red Mail", words: [0x2468, 0x6468, 0x2478, 0x6478]},
        ]
    },
];

// ---- abilities. The boots and the flippers also set the ability flag ($7EF379) the game checks:

export const abilities: Choice[] = [
    {
        name: "Dash (Pegasus Boots)", words: [0x3429, 0x342a, 0x342b, 0x342c],
        has: get => get(0xF355) !== 0,
        give: [set(0xF355, 1), or(0xF379, 0x04)],
        take: [set(0xF355, 0), and(0xF379, 0xFB)],
    },
    {
        name: "Lift 1 (Power Glove)", words: [0x2130, 0x2131, 0x2140, 0x2141],
        has: get => get(0xF354) >= 1,
        give: [set(0xF354, 1), {t: "gfx", w: "armor"}],
        take: [set(0xF354, 0), {t: "gfx", w: "armor"}],
    },
    {
        name: "Lift 2 (Titan's Mitt)", words: [0x28da, 0x28db, 0x28ea, 0x28eb],
        has: get => get(0xF354) >= 2,
        give: [set(0xF354, 2), {t: "gfx", w: "armor"}],
        // back to the Power Glove:
        take: [set(0xF354, 1), {t: "gfx", w: "armor"}],
    },
    {
        name: "Swim (Zora's Flippers)", words: [0x2c9a, 0x2c9b, 0x2c9d, 0x2c9e],
        has: get => get(0xF356) !== 0,
        give: [set(0xF356, 1), or(0xF379, 0x02)],
        take: [set(0xF356, 0), and(0xF379, 0xFD)],
    },
    {
        name: "Moon Pearl", words: [0x2433, 0x2434, 0x2435, 0x2436],
        has: get => get(0xF357) !== 0,
        give: [set(0xF357, 1)],
        take: [set(0xF357, 0)],
    },
];

// ---- half magic ($7EF37B: 0 = full cost, 1 = half, 2 = quarter):

export const halfMagic: Choice = {
    // the "1/2" of the HUD's magic meter; the third tile's gold pixel belongs to the item box and is hidden:
    name: "Half Magic", words: [0x28f7, 0x2851, 0x28fa], w: 3, hide: [HUD_GOLD],
    has: get => get(0xF37B) !== 0,
    give: [set(0xF37B, 1)],
    take: [set(0xF37B, 0)],
};

// ---- pendants ($7EF374) and crystals ($7EF37A), in one panel:

function bit(name: string, addr: number, mask: number, words: number[], w = 2): Choice {
    return {
        name, words, w,
        has: get => (get(addr) & mask) !== 0,
        give: [or(addr, mask)],
        take: [and(addr, ~mask & 0xFF)],
    };
}

export interface Placed {
    c: Choice;
    // the tile position in the panel:
    x: number;
    y: number;
    // what shows when the player doesn't have it; the item itself, faded, when not given:
    missing?: number[];
}

// The pendants in a row on top, and the crystals staggered below them from left to right:
export const PANEL_W = 10;
export const PANEL_H = 8;

const pendant = (name: string, mask: number, words: number[], x: number): Placed =>
    ({c: bit(name, 0xF374, mask, words), x, y: 1});
const CRYSTAL = [0x2d44, 0x2d45];
const CRYSTAL_MISSING = [0x3146, 0x3147];
// the crystals in the order of the bits of $7EF37A, with the dungeon that has each; they are laid out from the first
// dungeon to the last:
const CRYSTALS: [string, number][] = [
    ["Crystal 1 (Palace of Darkness)", 0x02],
    ["Crystal 2 (Swamp Palace)", 0x10],
    ["Crystal 3 (Skull Woods)", 0x40],
    ["Crystal 4 (Thieves' Town)", 0x20],
    ["Crystal 5 (Ice Palace)", 0x04],
    ["Crystal 6 (Misery Mire)", 0x01],
    ["Crystal 7 (Turtle Rock)", 0x08],
];

export const progressPanel: Placed[] = [
    pendant("Red Pendant", 0x01, [0x252b, 0x252c, 0x252d, 0x252e], 0.5),
    pendant("Blue Pendant", 0x02, [0x2d2b, 0x2d2c, 0x2d2d, 0x2d2e], 4),
    pendant("Green Pendant", 0x04, [0x3d2b, 0x3d2c, 0x3d2d, 0x3d2e], 7.7),
    ...CRYSTALS.map(([name, mask], i): Placed =>
        ({c: bit(name, 0xF37A, mask, CRYSTAL), x: 0.5 + i * 1.2, y: i % 2 === 0 ? 4 : 6, missing: CRYSTAL_MISSING})),
];

// ---- dungeons and their items ($7EF364 compass, $7EF366 big key, $7EF368 map: 16 bits, dungeon 0 is bit 15):

export const dungeons = [
    "Sewers", "Hyrule Castle", "Eastern Palace", "Desert Palace", "Agahnim's Tower", "Swamp Palace",
    "Palace of Darkness", "Misery Mire", "Skull Woods", "Ice Palace", "Tower of Hera", "Thieves' Town",
    "Turtle Rock", "Ganon's Tower",
];

export const BIG_KEY_WORDS = [0x28d6, 0x68d6, 0x28e6, 0x28e7];
export const MAP_WORDS = [0x28de, 0x28df, 0x28ee, 0x28ef];
export const COMPASS_WORDS = [0x24bf, 0x64bf, 0x2ccf, 0x6ccf];
export const SMALL_KEY_WORDS = [0x2871];
export const SMALL_KEY_FIRST = 0xF37C;
export const SMALL_KEY_LIVE = 0xF36F;

export function dungeonItem(name: string, base: number, words: number[], dungeon: number): Choice {
    // dungeon 0..7 are the bits 7..0 of the high byte, 8..15 are the bits 7..0 of the low byte:
    const addr = base + (dungeon < 8 ? 1 : 0);
    const mask = 0x80 >> (dungeon & 7);
    return bit(name, addr, mask, words);
}

export const dungeonItems = (dungeon: number): Choice[] => [
    dungeonItem("Map", 0xF368, MAP_WORDS, dungeon),
    dungeonItem("Compass", 0xF364, COMPASS_WORDS, dungeon),
    dungeonItem("Big Key", 0xF366, BIG_KEY_WORDS, dungeon),
];

// ---- hearts, magic and the fillable things:

export const HEART_FULL = [0x24a0];
export const HEART_HALF = [0x24a1];
export const HEART_EMPTY = [0x24a2];
export const RUPEE_WORDS = [0x3ca8];
export const BOMB_WORDS = [0x2c88, 0x2c89];
export const ARROW_WORDS = [0x20a7, 0x20a9];
// the full magic meter of the HUD, 3 tiles wide. The game's tiles of the meter's right side also hold a bit of the item
// box next to it, so the right side is the left side flipped:
export const MAGIC_METER_WORDS = [
    0x2850, 0xa856, 0x6850,
    0x2854, 0x3c4e, 0x6854,
    0x2854, 0x3c5e, 0x6854,
    0x2854, 0x3c5e, 0x6854,
    0x2854, 0x3c5e, 0x6854,
    0xa850, 0x2856, 0xe850,
];

// ---- progression flags ($7EF3C5, $7EF3C6, $7EF3C9 and the map markers $7EF3C7):

export const progressLevels = ["Start", "Uncle", "Zelda rescued", "Agahnim defeated"];

export interface Flag {
    name: string;
    addr: number;
    mask: number;
}

export const progressFlags: Flag[] = [
    {name: "Uncle check completed", addr: 0xF3C6, mask: 0x01},
    {name: "Priest's wishes started", addr: 0xF3C6, mask: 0x02},
    {name: "Zelda rescue completed", addr: 0xF3C6, mask: 0x04},
    {name: "Link left his bed", addr: 0xF3C6, mask: 0x10},
    {name: "Hobo check completed", addr: 0xF3C9, mask: 0x01},
    {name: "Bottle salesman check completed", addr: 0xF3C9, mask: 0x02},
    {name: "Flute boy completed", addr: 0xF3C9, mask: 0x08},
    {name: "Purple chest completed", addr: 0xF3C9, mask: 0x10},
    {name: "Smithy rescue completed", addr: 0xF3C9, mask: 0x20},
    {name: "Sword tempering started", addr: 0xF3C9, mask: 0x80},
];

export const mapMarkers = [
    "Castle", "Kakariko", "Sahasrahla", "Pendants", "Master Sword", "Agahnim's Tower", "Darkness", "Crystals",
    "Ganon's Tower",
];
