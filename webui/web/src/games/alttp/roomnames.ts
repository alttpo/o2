// The names of the underworld's rooms $000..$127, from ZScream's room names (ZScreamDungeon's
// ZeldaFullEditor/Utility/DefaultEntities.cs), with their typos fixed. A dungeon's rooms are named "<dungeon> - <room>".
import {dungeons} from "./debugdata";

export const roomNames: string[] = [
    "Ganon fight", // $000
    "Hyrule Castle - Back", // $001
    "Hyrule Castle - sanctuary back", // $002
    "Houlihan room", // $003
    "Turtle Rock - Crystaroller", // $004
    "Empty room", // $005
    "Swamp Palace - Arrghus", // $006
    "Tower of Hera - Moldorm", // $007
    "Fairy east of Sanctuary - Top", // $008
    "Palace of Darkness - Shooter room", // $009
    "Palace of Darkness - Stalfos basement", // $00A
    "Palace of Darkness - Monster truck room", // $00B
    "Ganon's Tower - Lobby", // $00C
    "Ganon's Tower - Agahnim 2", // $00D
    "Ice Palace - Entrance", // $00E
    "Empty room - Map Null", // $00F
    "Ganon fight fall", // $010
    "Hyrule Castle - End of Sewers", // $011
    "Hyrule Castle - Sanctuary", // $012
    "Turtle Rock - Pokeys 1", // $013
    "Turtle Rock - Lava Pipes", // $014
    "Turtle Rock - Cold Pipes", // $015
    "Swamp Palace - Current room", // $016
    "Tower of Hera - Floor 5", // $017
    "Fairy east of Sanctuary - Bottom", // $018
    "Palace of Darkness - Dark maze", // $019
    "Palace of Darkness - Falling bridge", // $01A
    "Palace of Darkness - Moving wall", // $01B
    "Ganon's Tower - Ice Armos", // $01C
    "Ganon's Tower - Penultima", // $01D
    "Ice Palace - Bombable floor", // $01E
    "Ice Palace - Penguin switch", // $01F
    "Agahnim's Tower - Agahnim", // $020
    "Hyrule Castle - Sewers key rat", // $021
    "Hyrule Castle - Sewers rat party", // $022
    "Turtle Rock - Laser camp entrance", // $023
    "Turtle Rock - Big chest", // $024
    "Empty room", // $025
    "Swamp Palace - Switch statue", // $026
    "Tower of Hera - Floor 4", // $027
    "Swamp Palace - Lobby", // $028
    "Skull Woods - Mothula", // $029
    "Palace of Darkness - Beetle arena", // $02A
    "Hookshot cave - Fairies", // $02B
    "Palace of Darkness - Sexy statue", // $02C
    "Room of Death", // $02D
    "Ice Palace - Penguins' chest", // $02E
    "Kakariko well", // $02F
    "Agahnim's Tower - Sacrificial altar", // $030
    "Tower of Hera - Floor 3", // $031
    "Hyrule Castle - Sewers cross", // $032
    "Desert Palace - Lanmolas", // $033
    "Swamp Palace - West wing blocks", // $034
    "Swamp Palace - West wing flood", // $035
    "Swamp Palace - Big chest lobby", // $036
    "Swamp Palace - East wing flood", // $037
    "Swamp Palace - East wing hall", // $038
    "Skull Woods - Fall to boss", // $039
    "Palace of Darkness - Main lobby", // $03A
    "Palace of Darkness - Hellway", // $03B
    "Hookshot cave - Booty room", // $03C
    "Ganon's Tower - Torches 2", // $03D
    "Ice Palace - Conveyor room", // $03E
    "Ice Palace - HP room", // $03F
    "Agahnim's Tower - Cat walk", // $040
    "Hyrule Castle - Secret Sewers", // $041
    "Hyrule Castle - Sewers snake orgy", // $042
    "Desert Palace - Moving wall", // $043
    "Thieves' Town - Basement conveyors", // $044
    "Thieves' Town - Basement prison", // $045
    "Swamp Palace - Split O", // $046
    "Empty room", // $047
    "Empty room", // $048
    "Skull Woods - Torches", // $049
    "Palace of Darkness - Entrance", // $04A
    "Palace of Darkness - Mimics warp", // $04B
    "Ganon's Tower - Bumpy conveyors", // $04C
    "Ganon's Tower - Moldorm 2", // $04D
    "Ice Palace - IPBJ", // $04E
    "Ice Palace - Clone with fairies", // $04F
    "Hyrule Castle - West wing back", // $050
    "Hyrule Castle - Throne room", // $051
    "Hyrule Castle - East wing back", // $052
    "Desert Palace - Popo genocide", // $053
    "Swamp Palace - Pick a hole", // $054
    "Hyrule Castle - Secret tunnel", // $055
    "Skull Woods - West exit", // $056
    "Skull Woods - Statue puzzle", // $057
    "Skull Woods - Exploding wall", // $058
    "Skull Woods - Bridges", // $059
    "Palace of Darkness - Helmasaur", // $05A
    "Ganon's Tower - Spike pegs", // $05B
    "Ganon's Tower - Cannon bridge", // $05C
    "Ganon's Tower - Gauntlets 1, 2, 3", // $05D
    "Ice Palace - Crossroads", // $05E
    "Ice Palace - Spiked floor", // $05F
    "Hyrule Castle - East wing entrance", // $060
    "Hyrule Castle - Lobby", // $061
    "Hyrule Castle - West wing entrance", // $062
    "Desert Palace - Back entrance", // $063
    "Thieves' Town - Attic west", // $064
    "Thieves' Town - Attic east", // $065
    "Swamp Palace - Waterfalls", // $066
    "Skull Woods - Firebars", // $067
    "Skull Woods - Bumper room", // $068
    "Empty room", // $069
    "Palace of Darkness - Dark rupees", // $06A
    "Ganon's Tower - Mimics fights", // $06B
    "Ganon's Tower - Lanmolas 2", // $06C
    "Ganon's Tower - Gauntlets 4, 5", // $06D
    "Ice Palace - Penguin lineup", // $06E
    "Empty room", // $06F
    "Hyrule Castle - Prison airlock", // $070
    "Hyrule Castle - Guarded chests", // $071
    "Hyrule Castle - Basement entrance", // $072
    "Desert Palace - Big chest", // $073
    "Desert Palace - North hall", // $074
    "Desert Palace - Cannon hall", // $075
    "Swamp Palace - Drainage rooms", // $076
    "Tower of Hera - Lobby", // $077
    "Empty room", // $078
    "Empty room", // $079
    "Empty room", // $07A
    "Ganon's Tower - Conveyor trolls", // $07B
    "Ganon's Tower - Falling basement bridge", // $07C
    "Ganon's Tower - Warp room A", // $07D
    "Ice Palace - Ice hall", // $07E
    "Ice Palace - Bari party", // $07F
    "Hyrule Castle - Basement prison", // $080
    "Hyrule Castle - Basement bridge", // $081
    "Hyrule Castle - Basement pit", // $082
    "Desert Palace - West entrance", // $083
    "Desert Palace - Main lobby", // $084
    "Desert Palace - East entrance", // $085
    "Empty room", // $086
    "Tower of Hera - Floor 1", // $087
    "Empty room", // $088
    "Eastern Palace - Fairies", // $089
    "Empty room", // $08A
    "Ganon's Tower - Pit block", // $08B
    "Ganon's Tower - Basement junction", // $08C
    "Ganon's Tower - Tile room puzzles", // $08D
    "Ice Palace - Blob party", // $08E
    "Empty room", // $08F
    "Misery Mire - Vitreous", // $090
    "Misery Mire - Basement blobs", // $091
    "Misery Mire - Dark room pegs", // $092
    "Misery Mire - Dark room shooters", // $093
    "Empty room", // $094
    "Ganon's Tower - Falling guard bridge", // $095
    "Ganon's Tower - Torches 1", // $096
    "Misery Mire - Moving wall", // $097
    "Misery Mire - Entrance", // $098
    "Eastern Palace - Dark eyegores", // $099
    "Empty room", // $09A
    "Ganon's Tower - Warp room B", // $09B
    "Ganon's Tower - Invisible maze", // $09C
    "Ganon's Tower - E", // $09D
    "Ice Palace - Puzzle junction", // $09E
    "Ice Palace - Pottery hunt", // $09F
    "Misery Mire - Penultima", // $0A0
    "Misery Mire - Fish room", // $0A1
    "Misery Mire - Bridges", // $0A2
    "Misery Mire - Waluigi bridge", // $0A3
    "Turtle Rock - Trinexx", // $0A4
    "Ganon's Tower - Wizzrobe fights", // $0A5
    "Ganon's Tower - Moldorm fall", // $0A6
    "Tower of Hera - Fairies", // $0A7
    "Eastern Palace - Stalfos room", // $0A8
    "Eastern Palace - Big chest", // $0A9
    "Eastern Palace - East hall", // $0AA
    "Thieves' Town - Spike room", // $0AB
    "Thieves' Town - Blind", // $0AC
    "Empty room", // $0AD
    "Ice Palace - Iced T", // $0AE
    "Ice Palace - Shittiest room", // $0AF
    "Agahnim's Tower - Circle of pots", // $0B0
    "Misery Mire - Hourglass", // $0B1
    "Misery Mire - Sluggula junction", // $0B2
    "Misery Mire - Spike chest", // $0B3
    "Turtle Rock - End of the line", // $0B4
    "Turtle Rock - Dark maze", // $0B5
    "Turtle Rock - Chomps", // $0B6
    "Turtle Rock - Roller chests", // $0B7
    "Eastern Palace - Antifairy chest", // $0B8
    "Eastern Palace - Cannon hall", // $0B9
    "Eastern Palace - Dark switch", // $0BA
    "Thieves' Town - Hellway", // $0BB
    "Thieves' Town - Toilet", // $0BC
    "Empty room", // $0BD
    "Ice Palace - Block switch", // $0BE
    "Ice Palace - Clone with switch", // $0BF
    "Agahnim's Tower - Dark pits", // $0C0
    "Misery Mire - Tile room", // $0C1
    "Misery Mire - Main lobby", // $0C2
    "Misery Mire - Falling bridge", // $0C3
    "Turtle Rock - Peg puzzle", // $0C4
    "Turtle Rock - Laser strip", // $0C5
    "Turtle Rock - Main lobby", // $0C6
    "Turtle Rock - Torch puzzle", // $0C7
    "Eastern Palace - Armos Knights", // $0C8
    "Eastern Palace - Entrance", // $0C9
    "Debug room", // $0CA
    "Thieves' Town - Northwest lobby", // $0CB
    "Thieves' Town - Northeast lobby", // $0CC
    "Empty room", // $0CD
    "Ice Palace - Fall to boss", // $0CE
    "Empty room", // $0CF
    "Agahnim's Tower - Dark maze", // $0D0
    "Misery Mire - Wall puzzle weekends", // $0D1
    "Misery Mire - Entry gauntlet", // $0D2
    "Empty room", // $0D3
    "Empty room", // $0D4
    "Turtle Rock - Laser bridge", // $0D5
    "Turtle Rock - Entrance", // $0D6
    "Empty room", // $0D7
    "Eastern Palace - Gauntlet", // $0D8
    "Eastern Palace - Cannon room", // $0D9
    "Eastern Palace - Upstairs antifairies", // $0DA
    "Thieves' Town - Southwest lobby", // $0DB
    "Thieves' Town - Southeast lobby", // $0DC
    "Empty room", // $0DD
    "Ice Palace - Kholdstare", // $0DE
    "Paradox Cave - Summit exit", // $0DF
    "Agahnim's Tower - Entrance", // $0E0
    "Lost Woods cave", // $0E1
    "Lumberjack Bumpkins' cave", // $0E2
    "Magic bat cave", // $0E3
    "Old Man's house cave", // $0E4
    "Old Man's house cave east", // $0E5
    "Death Mountain Descent - West", // $0E6
    "Death Mountain Descent - East", // $0E7
    "Superbunny cave - Top", // $0E8
    "Empty room", // $0E9
    "Spectacle Rock cave - Top", // $0EA
    "Bumper cave - Top", // $0EB
    "Empty room", // $0EC
    "Death Mountain fairy cave - Top", // $0ED
    "Spiral cave - top", // $0EE
    "Paradox cave - Moldorm chests", // $0EF
    "Lost Old Man's cave - West", // $0F0
    "Lost Old Man's cave - East", // $0F1
    "Sahasrahla's house - West", // $0F2
    "Sahasrahla's house - East", // $0F3
    "Feuding brothers - West", // $0F4
    "Feuding brothers - East", // $0F5
    "Empty room", // $0F6
    "Empty room", // $0F7
    "Superbunny cave - Bottom", // $0F8
    "Spectacle Rock cave - Bottom, west", // $0F9
    "Spectacle Rock cave - Bottom, East", // $0FA
    "Bumper cave - Bottom", // $0FB
    "Empty room", // $0FC
    "Death Mountain fairy cave - Bottom", // $0FD
    "Spiral cave - Bottom", // $0FE
    "Paradox cave - Shop", // $0FF
    "Boring chest game", // $100
    "Kakariko houses", // $101
    "Sick kid's house", // $102
    "Kakariko hospitality facilities", // $103
    "Link's house", // $104
    "Sahasrahla's hideout", // $105
    "Fun chest game", // $106
    "Library and secret pots", // $107
    "Chicken hut", // $108
    "Potion shop", // $109
    "Aginah's hideout", // $10A
    "Dam control room", // $10B
    "Mimic cave", // $10C
    "Mire shed", // $10D
    "Two hints", // $10E
    "Dark World shop", // $10F
    "Dark World shop", // $110
    "Arrow game", // $111
    "Dark sanc and shop", // $112
    "King's tomb", // $113
    "Waterfall fairy and Watto", // $114
    "Pond of Happiness", // $115
    "Fat Fairy", // $116
    "Spike Cave", // $117
    "Unfun chest game", // $118
    "Blind's hideout - Upstairs", // $119
    "Broccoli's hideout", // $11A
    "Grove and Grave caves", // $11B
    "Bomb shop and C-house", // $11C
    "Blind's hideout - Basement", // $11D
    "Fairies and Thief cave", // $11E
    "Not notable shop", // $11F
    "Ice cave", // $120
    "Dwarven smithery", // $121
    "Fortune Tellers x2", // $122
    "Mini moldorm cave", // $123
    "Rich pots and Bonk rocks", // $124
    "Small rupee cache and Mini spike cave", // $125
    "Big fairy and Checkerboard cave", // $126
    "Stake puzzle and unused spike cave", // $127
];

// placeNames is the dungeon (from its $040C, palace, which is $FF for caves and houses) and the name of an underworld
// room. A cave or house is named by the room's name; a dungeon's room name leaves out its dungeon.
export function placeNames(room: number, palace: number | undefined): {dungeon: string, name: string} {
    const full = roomNames[room] ?? "";
    const dash = full.indexOf(" - ");
    let dungeon = palace !== undefined && palace < 0xFF ? dungeons[palace >> 1] ?? "" : "";
    if (palace === undefined && dash >= 0 && dungeons.includes(full.slice(0, dash))) {
        dungeon = full.slice(0, dash);
    }
    let name = full;
    if (dungeon && dash >= 0) {
        const prefix = full.slice(0, dash);
        // the sewers are Hyrule Castle's:
        if (prefix === dungeon || (prefix === "Hyrule Castle" && dungeon === "Sewers")) name = full.slice(dash + 3);
    }
    if (!dungeon) dungeon = palace === 0xFF ? "Cave or house" : "";
    return {dungeon, name};
}
