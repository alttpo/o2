// The alttp game view's sections of what the player has: what to fill up, Y items, equipment, abilities, dungeon
// items, pendants and crystals, and heart containers and pieces. Each icon is drawn from the game's own graphics (see
// debuggfx.tsx).
//
// They only show what the player has until debug mode is switched on from the /debug page. In debug mode clicking
// them gives and takes away items, and the progression, enemies and warp sections are shown too.

import {Fragment, h} from "preact";
import {useState} from "preact/hooks";
import {GameALTTPViewModel, GameViewProps} from "../../viewmodel";
import {DebugOp, Gfx, Icon, useGfx} from "./debuggfx";
import {targetText, warpOp, WarpTarget} from "./warpmap";
import {
    abilities, ARROW_MAX, ARROW_WORDS, BOMB_MAX, BOMB_WORDS, BOTTLE_ADDR, BOTTLE_CELL, BOTTLE_COUNT, BOTTLE_SELECTED, bottleContents,
    bottleCycle, Choice, dungeonItems, dungeons, equipment, Getter, halfMagic, HEART_FULL,
    HEART_HALF, MAGIC_METER_WORDS, mapMarkers, PANEL_H, placeGroups, PANEL_W, Placed, progressPanel, progressFlags, progressLevels,
    RUPEE_WORDS, SMALL_KEY_LIVE, SMALL_KEY_WORDS, SRAM_FIRST, yGrid
} from "./debugdata";

// the "game/debug" view model:
export interface DebugState {
    sram: number[];
    // $040C: the dungeon Link is in, $FF in the overworld
    dungeon: number;
    inGame: boolean;
}

type Send = (...ops: DebugOp[]) => void;

interface Common {
    gfx: Gfx | null;
    get: Getter;
    send: Send;
    // whether the game is running, so that a change is applied:
    ready: boolean;
    // debug mode: whether clicking changes things. Without it nothing is clickable and nothing says it could be:
    edit: boolean;
}

interface CellProps {
    edit: boolean;
    cls: string;
    disabled?: boolean;
    // the tooltip in debug mode, which says what a click does:
    title?: string;
    // the tooltip outside debug mode, which only names what is shown:
    info?: string;
    style?: string;
    onClick?: (e: MouseEvent) => void;
    children?: any;
}

// Cell is a button in debug mode, and otherwise a plain box that looks the same but does nothing.
function Cell({edit, cls, disabled, title, info, style, onClick, children}: CellProps) {
    return edit
        ? <button type="button" class={cls} disabled={disabled} title={title} style={style} onClick={onClick}>{children}</button>
        : <div class={cls} title={info} style={style}>{children}</div>;
}

// ChoiceButton gives the item if the player doesn't have it, and takes it away if they do. Names are in the tooltip.
function ChoiceButton({c, gfx, get, send, ready, edit, scale = 3}: Common & { c: Choice, scale?: number }) {
    const has = c.has(get);
    return (
        <Cell edit={edit} cls={"dbg-btn" + (has ? " on" : "")} disabled={!ready}
              title={(has ? "Remove " : "Give ") + c.name} info={c.name}
              onClick={() => send(...(has ? c.take : c.give))}>
            <Icon gfx={gfx} words={c.words} w={c.w} scale={scale} dim={!has} hide={c.hide}/>
        </Cell>
    );
}

export function Section({title, note, children}: { title: string, note?: string, children?: any }) {
    return (
        <div class="content dbg-section">
            <h3>{title}</h3>
            {note && <div class="dbg-note">{note}</div>}
            {children}
        </div>
    );
}

// useBottles has the bottle logic. A bottle slot is available when it holds something ($7EF35C..$7EF35F); the
// bottle selected ($7EF34F) must be an available one, or 0 without any.
function useBottles(get: Getter, send: Send) {
    const slots = Array.from({length: BOTTLE_COUNT}, (_, i) => get(BOTTLE_ADDR + i));
    const count = slots.filter(v => v !== 0).length;
    const selected = get(BOTTLE_SELECTED);
    const selectedIndex = selected >= 1 && selected <= BOTTLE_COUNT && slots[selected - 1] !== 0 ? selected - 1 : slots.findIndex(v => v !== 0);

    const apply = (after: number[]) => {
        const ops: DebugOp[] = [];
        after.forEach((v, i) => {
            if (v !== slots[i]) ops.push({t: "set", a: BOTTLE_ADDR + i, v});
        });
        const keep = selected >= 1 && selected <= BOTTLE_COUNT && after[selected - 1] !== 0;
        const sel = keep ? selected : after.findIndex(v => v !== 0) + 1;
        if (sel !== selected) ops.push({t: "set", a: BOTTLE_SELECTED, v: sel});
        if (ops.length) send(...ops);
    };

    // the first n slots are the available ones, keeping what they hold and giving empty bottles to new ones:
    const setCount = (n: number) => apply(slots.map((v, i) => i >= n ? 0 : v === 0 ? 2 : v));

    return {
        slots, count, selectedIndex,
        // steps the number of bottles, wrapping around from 4 to none:
        stepCount: (dir: number) => setCount((count + dir + BOTTLE_COUNT + 1) % (BOTTLE_COUNT + 1)),
        // cycles what a slot holds through the contents:
        cycle: (i: number, dir: number) => {
            if (slots[i] === 0) return;
            const at = Math.max(0, bottleCycle.indexOf(slots[i]));
            const next = bottleCycle[(at + dir + bottleCycle.length) % bottleCycle.length];
            apply(slots.map((v, k) => k === i ? next : v));
        },
    };
}

type Bottles = ReturnType<typeof useBottles>;

// Both draws two icons on either side of a diagonal line, for a slot that has both of its items.
function Both({gfx, words, words2}: { gfx: Gfx | null, words: number[], words2: number[] }) {
    return (
        <div class="dbg-both">
            <div class="dbg-half dbg-half-1"><Icon gfx={gfx} words={words}/></div>
            <div class="dbg-half dbg-half-2"><Icon gfx={gfx} words={words2}/></div>
            <div class="dbg-diagonal"/>
        </div>
    );
}

function YGrid({gfx, get, send, ready, edit, bottles}: Common & { bottles: Bottles }) {
    const {slots, count, selectedIndex} = bottles;
    return (
        <Section title="Y Items"
                 note={edit ? "Click an item to go to its next level; shift-click to go back. The bottle steps through 0 to 4 bottles." : undefined}>
            <div class="dbg-yitems">
                <div class="dbg-ygrid">
                    {yGrid.map(c => {
                        if (c.name === BOTTLE_CELL) {
                            const v = selectedIndex >= 0 ? slots[selectedIndex] : 0;
                            const words = (bottleContents[v] ?? bottleContents[2]).words;
                            const around = BOTTLE_COUNT + 1;
                            return (
                                <Cell key={c.name} edit={edit} cls={"dbg-btn" + (count > 0 ? " on" : "")} disabled={!ready}
                                      title={count + " of " + BOTTLE_COUNT + " bottles: click for " + (count + 1) % around +
                                          ", shift-click for " + (count + BOTTLE_COUNT) % around}
                                      info={count + " of " + BOTTLE_COUNT + " bottles"}
                                      onClick={e => bottles.stepCount(e.shiftKey ? -1 : 1)}>
                                    <Icon gfx={gfx} words={v !== 0 ? words : bottleContents[2].words} dim={count === 0}/>
                                </Cell>
                            );
                        }
                        const cur = c.current(get);
                        const next = (dir: number) => c.levels[(cur + dir + c.levels.length) % c.levels.length];
                        const shown = c.levels[cur === 0 ? 1 : cur];
                        return (
                            <Cell key={c.name} edit={edit} cls={"dbg-btn" + (cur !== 0 ? " on" : "")} disabled={!ready}
                                  title={c.name + ": " + c.levels[cur].name + " (click for " + next(1).name + ")"}
                                  info={c.name + ": " + c.levels[cur].name}
                                  onClick={e => send(...next(e.shiftKey ? -1 : 1).ops)}>
                                {shown.words2
                                    ? <Both gfx={gfx} words={shown.words} words2={shown.words2}/>
                                    : <Icon gfx={gfx} words={shown.words} dim={cur === 0}/>}
                            </Cell>
                        );
                    })}
                </div>
                <div class="dbg-bottles">
                    {slots.map((v, i) => (
                        <Cell key={i} edit={edit}
                              cls={"dbg-btn" + (v !== 0 ? " on" : "") + (i === selectedIndex ? " selected" : "")}
                              disabled={!ready || v === 0}
                              title={v === 0 ? "Bottle " + (i + 1) + ": not available" : "Bottle " + (i + 1) + ": " + bottleContents[v]?.name +
                                  (i === selectedIndex ? " (selected)" : "") + ". Click for the next contents, shift-click for the previous"}
                              info={v === 0 ? "Bottle " + (i + 1) + ": not available" : "Bottle " + (i + 1) + ": " + bottleContents[v]?.name +
                                  (i === selectedIndex ? " (selected)" : "")}
                              onClick={e => bottles.cycle(i, e.shiftKey ? -1 : 1)}>
                            <Icon gfx={gfx} words={(v !== 0 ? bottleContents[v] ?? bottleContents[2] : bottleContents[2]).words} dim={v === 0}/>
                        </Cell>
                    ))}
                </div>
            </div>
        </Section>
    );
}

// the value of no sword that the game uses, 0 or $FF, as last seen:
let swordNone = 0;

// Equipment is laid out as the item menu does: sword, shield and armor in a row.
function Equipment({gfx, get, send, ready, edit}: Common) {
    return (
        <Section title="Equipment" note={edit ? "Click to upgrade, wrapping around; shift-click to downgrade." : undefined}>
            <div class="dbg-grid">
                {equipment.map(e => {
                    const v = get(e.addr);
                    // the randomizer's no sword is $FF, as the smithy's while the sword is tempered, where the original
                    // game's is 0; $FF is not a level:
                    const cur = v < e.levels.length ? v : 0;
                    if (e.gfx === "sword" && (v === 0 || v === 0xFF)) swordNone = v;
                    const to = (level: number) => send({t: "set", a: e.addr, v: e.gfx === "sword" && level === 0 ? swordNone : level},
                        {t: "gfx", w: e.gfx});
                    const n = e.levels.length;
                    return (
                        <Cell key={e.name} edit={edit} cls="dbg-btn on" disabled={!ready}
                              title={e.name + " $" + v.toString(16).toUpperCase().padStart(2, "0") + ": " + e.levels[cur].name + " (click for " + e.levels[(cur + 1) % n].name + ")"}
                              info={e.name + ": " + e.levels[cur].name}
                              onClick={ev => to((cur + (ev.shiftKey ? n - 1 : 1)) % n)}>
                            <Icon gfx={gfx} words={e.levels[cur].words}/>
                        </Cell>
                    );
                })}
            </div>
        </Section>
    );
}

function Abilities(p: Common) {
    return (
        <Section title="Abilities">
            <div class="dbg-grid">
                {abilities.map(c => <ChoiceButton key={c.name} c={c} {...p}/>)}
                <ChoiceButton c={halfMagic} {...p}/>
            </div>
        </Section>
    );
}

const PANEL_SCALE = 3;
const TILE = 8 * PANEL_SCALE;

// Panel places icons at the tile positions of the item menu's progress panel:
function Panel({items, gfx, get, send, ready, edit}: Common & { items: Placed[] }) {
    return (
        <div class="dbg-panel" style={`width: ${PANEL_W * TILE}px; height: ${PANEL_H * TILE}px`}>
            {items.map(it => {
                const has = it.c.has(get);
                return (
                    <Cell key={it.c.name} edit={edit} cls={"dbg-btn dbg-bare" + (has ? " on" : "")} disabled={!ready}
                          title={(has ? "Remove " : "Give ") + it.c.name} info={it.c.name}
                          style={`left: ${it.x * TILE - 3}px; top: ${it.y * TILE - 3}px`}
                          onClick={() => send(...(has ? it.c.take : it.c.give))}>
                        <Icon gfx={gfx} words={has || !it.missing ? it.c.words : it.missing} w={it.c.w} scale={PANEL_SCALE}
                              dim={!has && !it.missing}/>
                    </Cell>
                );
            })}
        </div>
    );
}

function PendantsAndCrystals(p: Common) {
    return (
        <Section title="Pendants and Crystals">
            <Panel items={progressPanel} {...p}/>
        </Section>
    );
}

interface Step {
    title: string;
    disabled?: boolean;
    // `big` when shift is held:
    onClick: (big: boolean) => void;
}

interface CounterProps {
    gfx: Gfx | null;
    words: number[];
    w?: number;
    scale?: number;
    // what clicking the icon does, if anything:
    onMain?: () => void;
    mainTitle?: string;
    text: any;
    textTitle: string;
    dec: Step;
    inc: Step;
    // the whole card is out of use:
    off?: boolean;
    ready: boolean;
    edit: boolean;
}

// Counter is a card with an icon, a count and, in debug mode, buttons to take one away and give one. Clicking the
// icon does the main thing, which for the consumables is to fill up.
function Counter({gfx, words, w = 1, scale = 5, onMain, mainTitle, text, textTitle, dec, inc, off = false, ready, edit}: CounterProps) {
    const idle = !ready || off;
    const icon = <Icon gfx={gfx} words={words} w={w} scale={scale} dim={off}/>;
    return (
        <div class={"dbg-btn dbg-static" + (off ? " off" : "")}>
            {edit
                ? <button type="button" class="dbg-main" disabled={idle || !onMain} title={mainTitle} onClick={onMain}>{icon}</button>
                : <div class="dbg-main">{icon}</div>}
            <span class="dbg-count" title={textTitle}>{text}</span>
            {edit && <span class="dbg-step">
                <button type="button" disabled={idle || dec.disabled} title={dec.title}
                        onClick={e => dec.onClick(e.shiftKey)}>−</button>
                <button type="button" disabled={idle || inc.disabled} title={inc.title}
                        onClick={e => inc.onClick(e.shiftKey)}>+</button>
            </span>}
        </div>
    );
}

function DungeonItems({gfx, get, send, ready, edit, state}: Common & { state?: DebugState }) {
    // the dungeon chosen, or -1 for the one Link is in:
    const [chosen, setChosen] = useState(-1);
    const inDungeon = !!state && state.dungeon !== 0xFF && (state.dungeon >> 1) < dungeons.length;
    const current = inDungeon ? state!.dungeon >> 1 : -1;
    const d = chosen >= 0 ? chosen : current;

    const keys = inDungeon ? get(SMALL_KEY_LIVE) : -1;

    return (
        <Section title="Dungeon Items" note="Big keys, maps and compasses of a dungeon, and the small keys of the dungeon Link is in.">
            <div class="dbg-row">
                <label for="dbg-dungeon">Dungeon:&nbsp;</label>
                <select id="dbg-dungeon" value={chosen}
                        onChange={e => setChosen(parseInt((e.target as HTMLSelectElement).value, 10))}>
                    <option value={-1}>Current{inDungeon ? " (" + dungeons[current] + ")" : " (not in a dungeon)"}</option>
                    {dungeons.map((n, i) => <option key={i} value={i}>{n}</option>)}
                </select>
            </div>
            <div class="dbg-grid dbg-sub">
                <Counter gfx={gfx} ready={ready} edit={edit} words={SMALL_KEY_WORDS} scale={6}
                         text={keys >= 0 && keys !== 0xFF ? keys : "-"} textTitle="Small keys of the dungeon Link is in"
                         dec={{title: "Take a small key", disabled: !inDungeon, onClick: () => send({t: "keys", d: -1})}}
                         inc={{title: "Give a small key", disabled: !inDungeon, onClick: () => send({t: "keys", d: 1})}}/>
                {d >= 0 && dungeonItems(d).map(c => (
                    <ChoiceButton key={c.name} c={c} gfx={gfx} get={get} send={send} ready={ready} edit={edit}/>
                ))}
            </div>
        </Section>
    );
}

function Hearts({gfx, get, send, ready, edit}: Common) {
    const containers = get(0xF36C) >> 3;
    const pieces = get(0xF36B) & 3;
    const step = (c: number, p: number) => send({t: "hearts", c, p});
    const full = containers >= 20;
    return (
        <Section title="Heart Containers and Pieces" note={edit ? "Click the heart to have 20 full hearts." : undefined}>
            <div class="dbg-row">
                <Counter gfx={gfx} ready={ready} edit={edit} words={HEART_FULL}
                         onMain={() => send({t: "set", a: 0xF36C, v: 0xA0}, {t: "set", a: 0xF36D, v: 0xA0})}
                         mainTitle="Give heart containers up to 20 hearts and fill them up"
                         text={containers} textTitle="Heart containers"
                         dec={{title: "Remove a heart container", disabled: containers <= 1, onClick: () => step(-1, 0)}}
                         inc={{title: "Give a heart container", disabled: full, onClick: () => step(1, 0)}}/>
                <Counter gfx={gfx} ready={ready} edit={edit} words={HEART_HALF} off={full}
                         text={pieces + "/4"} textTitle="Heart pieces, 4 make a container"
                         dec={{title: "Remove a heart piece", disabled: containers <= 1 && pieces === 0, onClick: () => step(0, -1)}}
                         inc={{title: "Give a heart piece", onClick: () => step(0, 1)}}/>
            </div>
        </Section>
    );
}

// the most rupees the wallet holds:
const RUPEE_MAX = 999;

function Fill({gfx, get, send, ready, edit}: Common) {
    const hearts = get(0xF36D);
    const heartCap = get(0xF36C);
    const magic = get(0xF36E);
    // $7EF360 is the number the rupees count up or down to:
    const rupees = get(0xF360) | (get(0xF361) << 8);
    const arrows = get(0xF377);
    const arrowCap = ARROW_MAX[Math.min(get(0xF371), 7)];
    const bombs = get(0xF343);
    const bombCap = BOMB_MAX[Math.min(get(0xF370), 7)];

    const clamp = (v: number, lo: number, hi: number) => Math.max(lo, Math.min(hi, v));
    const setRupees = (v: number) => send({t: "set", a: 0xF360, v: v & 0xFF}, {t: "set", a: 0xF361, v: v >> 8});
    const fill = (w: string) => () => send({t: "fill", w});
    const p = {gfx, ready, edit};

    return (
        <Section title="Consumables"
                 note={edit ? "Click an icon to fill it up. The buttons change it by one; shift-click for 20 rupees, 10 arrows or bombs, or 32 magic." : undefined}>
            <div class="dbg-grid">
                <Counter {...p} words={HEART_FULL} onMain={fill("hearts")} mainTitle="Fill up the hearts"
                         text={+(hearts / 8).toFixed(2)} textTitle="Hearts"
                         dec={{title: "Take away a heart", disabled: hearts <= 8,
                             onClick: () => send({t: "set", a: 0xF36D, v: clamp(hearts - 8, 8, heartCap)})}}
                         inc={{title: "Give a heart", disabled: hearts >= heartCap,
                             onClick: () => send({t: "set", a: 0xF36D, v: clamp(hearts + 8, 8, heartCap)})}}/>
                <Counter {...p} words={MAGIC_METER_WORDS} w={3} scale={3} onMain={fill("magic")} mainTitle="Fill up the magic"
                         text={magic} textTitle="Magic, of 128"
                         dec={{title: "Use up 8 magic (shift: 32)", disabled: magic <= 0,
                             onClick: big => send({t: "set", a: 0xF36E, v: clamp(magic - (big ? 32 : 8), 0, 128)})}}
                         inc={{title: "Give 8 magic (shift: 32)", disabled: magic >= 128,
                             onClick: big => send({t: "set", a: 0xF36E, v: clamp(magic + (big ? 32 : 8), 0, 128)})}}/>
                <Counter {...p} words={RUPEE_WORDS} onMain={fill("rupees")} mainTitle="Fill up the rupees (999)"
                         text={rupees} textTitle="Rupees"
                         dec={{title: "Take away a rupee (shift: 20)", disabled: rupees <= 0,
                             onClick: big => setRupees(clamp(rupees - (big ? 20 : 1), 0, RUPEE_MAX))}}
                         inc={{title: "Give a rupee (shift: 20)", disabled: rupees >= RUPEE_MAX,
                             onClick: big => setRupees(clamp(rupees + (big ? 20 : 1), 0, RUPEE_MAX))}}/>
                <Counter {...p} words={ARROW_WORDS} w={2} scale={3} onMain={fill("arrows")} mainTitle="Fill up the arrows"
                         text={arrows} textTitle={"Arrows, of " + arrowCap}
                         dec={{title: "Take away an arrow (shift: 10)", disabled: arrows <= 0,
                             onClick: big => send({t: "set", a: 0xF377, v: clamp(arrows - (big ? 10 : 1), 0, arrowCap)})}}
                         inc={{title: "Give an arrow (shift: 10)", disabled: arrows >= arrowCap,
                             onClick: big => send({t: "set", a: 0xF377, v: clamp(arrows + (big ? 10 : 1), 0, arrowCap)})}}/>
                <Counter {...p} words={BOMB_WORDS} w={2} scale={3} onMain={fill("bombs")} mainTitle="Fill up the bombs"
                         text={bombs} textTitle={"Bombs, of " + bombCap}
                         dec={{title: "Take away a bomb (shift: 10)", disabled: bombs <= 0,
                             onClick: big => send({t: "set", a: 0xF343, v: clamp(bombs - (big ? 10 : 1), 0, bombCap)})}}
                         inc={{title: "Give a bomb (shift: 10)", disabled: bombs >= bombCap,
                             onClick: big => send({t: "set", a: 0xF343, v: clamp(bombs + (big ? 10 : 1), 0, bombCap)})}}/>
            </div>
        </Section>
    );
}

function Progress({get, send, ready}: Common) {
    const level = get(0xF3C5);
    const markers = get(0xF3C7);
    return (
        <Section title="Progression"
                 note="Flags the game keeps of the story. Some only take effect when the room or area loads again.">
            <div class="dbg-row">
                <span class="dbg-row-label">Story</span>
                {progressLevels.map((n, i) => (
                    <button type="button" key={i} class={"dbg-chip" + (level === i ? " on" : "")} disabled={!ready}
                            title={"Set the progress to " + n} onClick={() => send({t: "progress", v: i})}>
                        {n}
                    </button>
                ))}
            </div>
            <div class="dbg-row">
                <span class="dbg-row-label">Map markers</span>
                <select value={Math.min(markers, mapMarkers.length - 1)} disabled={!ready}
                        onChange={e => send({t: "set", a: 0xF3C7, v: parseInt((e.target as HTMLSelectElement).value, 10)})}>
                    {mapMarkers.map((n, i) => <option key={i} value={i}>{n}</option>)}
                </select>
            </div>
            <div class="dbg-grid dbg-sub">
                {progressFlags.map(f => {
                    const set = (get(f.addr) & f.mask) !== 0;
                    return (
                        <button type="button" key={f.name} class={"dbg-chip" + (set ? " on" : "")} disabled={!ready}
                                title={(set ? "Clear" : "Set") + " the flag: " + f.name}
                                onClick={() => send(set
                                    ? {t: "and", a: f.addr, m: ~f.mask & 0xFF}
                                    : {t: "or", a: f.addr, m: f.mask})}>
                            {set ? "☑ " : "☐ "}{f.name}
                        </button>
                    );
                })}
            </div>
        </Section>
    );
}

function Enemies({send, ready}: Common) {
    return (
        <Section title="Enemies">
            <button type="button" disabled={!ready} onClick={() => send({t: "kill"})}
                    title="Enemies turn into the death poof. Items and NPCs are left alone.">
                Kill all enemies on screen
            </button>
        </Section>
    );
}

interface WarpProps extends Common {
    warpTarget: WarpTarget | null;
    setWarpTarget: (t: WarpTarget | null) => void;
}

// Warp sends Link through a place's door, into the underworld at its entrance or out onto the overworld at its exit,
// or to the point of an underworld room or an overworld area picked on the map.
function Warp({send, ready, warpTarget, setWarpTarget}: WarpProps) {
    // the group and the index of the place chosen:
    const [chosen, setChosen] = useState("0:0");
    const [g, i] = chosen.split(":").map(n => parseInt(n, 10));
    const pl = placeGroups[g]?.places[i];
    return (
        <Section title="Warp"
                 note="Link goes through the iris like through a door. Warps wait for Link to stand in the overworld or the underworld.">
            <div class="dbg-row">
                <select value={chosen} onChange={e => setChosen((e.target as HTMLSelectElement).value)}>
                    {placeGroups.map((grp, gi) => (
                        <optgroup key={grp.name} label={grp.name}>
                            {grp.places.map((p, pi) => <option key={p.name} value={gi + ":" + pi}>{p.name}</option>)}
                        </optgroup>
                    ))}
                </select>
            </div>
            <div class="dbg-row">
                <button type="button" disabled={!ready || !pl}
                        title={pl && "Enter " + pl.name + " in the underworld"}
                        onClick={() => pl && send({t: "warp", w: "entrance", e: pl.entrance, v: pl.exit})}>
                    Underworld entrance
                </button>
                <button type="button" disabled={!ready || !pl}
                        title={pl && "Leave " + pl.name + " onto the overworld"}
                        onClick={() => pl && send({t: "warp", w: "exit", v: pl.exit})}>
                    Overworld exit
                </button>
            </div>
            <div class="dbg-row">
                <span class="dbg-sub" title="Pick a point of an underworld room or an overworld area on the map below; Escape or a right click on the map clears it">
                    {warpTarget ? "Map point: " + targetText(warpTarget) : "Map point: pick one in an underworld room or an overworld area on the map (double-click to warp there)"}
                </span>
            </div>
            <div class="dbg-row">
                <button type="button" disabled={!ready || !warpTarget}
                        title={warpTarget?.kind === "area" ? "Leave for the area as from a cave, at the point"
                            : "Enter the room's dungeon if Link isn't in it, then drop into the room at the point"}
                        onClick={() => warpTarget && send(warpOp(warpTarget))}>
                    Warp to map point
                </button>
                {warpTarget && <button type="button" onClick={() => setWarpTarget(null)}>Clear</button>}
            </div>
        </Section>
    );
}

// DebugSections shows what the player has in the alttp game view, and in debug mode changes it.
export function DebugSections({ch, vm, warpTarget, setWarpTarget}: GameViewProps & {
    warpTarget: WarpTarget | null;
    setWarpTarget: (t: WarpTarget | null) => void;
}) {
    const game = vm.game as GameALTTPViewModel;
    const edit = !!game.debugMode;
    const state = vm["game/debug"] as DebugState | undefined;
    const {gfx, error} = useGfx(game.isCreated);

    const sram = state?.sram;
    const get: Getter = addr => sram?.[addr - SRAM_FIRST] ?? 0;
    const send: Send = (...ops) => {
        if (edit) ch.command("game", "debug", {ops});
    };
    const ready = !!state?.inGame;

    const p: Common = {gfx, get, send, ready, edit};
    const bottles = useBottles(get, send);

    let status = "";
    if (!state) {
        status = "Waiting for the game: connect the SNES and start the patched ROM.";
    } else if (!state.inGame) {
        status = "Link is not in the game right now (title, file select or a transition); the buttons wait for it.";
    }

    return (
        <Fragment>
            {edit && <div class="dbg-note">
                Debug mode: changes are made by the game itself within a frame. Like anything the player does, they are
                shared with the group by the sync settings.
                {status && <div style="color: #ffb000">{status}</div>}
            </div>}
            {error && <div style="color: red; font-size: smaller">Graphics: {error}</div>}
            <div class="dbg-columns">
                <Fill {...p}/>
                <YGrid {...p} bottles={bottles}/>
                <Equipment {...p}/>
                <Abilities {...p}/>
                <DungeonItems {...p} state={state}/>
                <PendantsAndCrystals {...p}/>
                <Hearts {...p}/>
                {edit && <Progress {...p}/>}
                {edit && <Enemies {...p}/>}
                {edit && <Warp {...p} warpTarget={warpTarget} setWarpTarget={setWarpTarget}/>}
            </div>
        </Fragment>
    );
}
