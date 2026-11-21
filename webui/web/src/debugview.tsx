// The /debug page: tools to give and take away items, fill up the player and kill enemies while debugging.
// Each icon is drawn from the game's own graphics (see debuggfx.tsx).

import {Fragment, h} from "preact";
import {useState} from "preact/hooks";
import {ConnectionLost, useConnection} from "./connection";
import {DebugOp, Gfx, Icon, useGfx} from "./debuggfx";
import {
    abilities, ARROW_WORDS, BOMB_WORDS, BOTTLE_ADDR, BOTTLE_CELL, BOTTLE_COUNT, BOTTLE_SELECTED, bottleContents,
    bottleCycle, Choice, dungeonItems, dungeons, equipment, Getter, halfMagic, HEART_FULL,
    HEART_HALF, MAGIC_METER_WORDS, mapMarkers, PANEL_H, PANEL_W, Placed, progressPanel, progressFlags, progressLevels,
    RUPEE_WORDS, SMALL_KEY_LIVE, SMALL_KEY_WORDS, SRAM_FIRST, yGrid
} from "./debugdata";

// the "game/debug" view model:
interface DebugState {
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
}

// ChoiceButton gives the item if the player doesn't have it, and takes it away if they do. Names are in the tooltip.
function ChoiceButton({c, gfx, get, send, ready, scale = 3}: Common & { c: Choice, scale?: number }) {
    const has = c.has(get);
    return (
        <button type="button" class={"dbg-btn" + (has ? " on" : "")} disabled={!ready}
                title={(has ? "Remove " : "Give ") + c.name}
                onClick={() => send(...(has ? c.take : c.give))}>
            <Icon gfx={gfx} words={c.words} w={c.w} scale={scale} dim={!has} hide={c.hide}/>
        </button>
    );
}

function Section({title, note, children}: { title: string, note?: string, children?: any }) {
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

function YGrid({gfx, get, send, ready, bottles}: Common & { bottles: Bottles }) {
    const {slots, count, selectedIndex} = bottles;
    return (
        <Section title="Y Items" note="Click an item to go to its next level; shift-click to go back. The bottle steps through 0 to 4 bottles.">
            <div class="dbg-yitems">
                <div class="dbg-ygrid">
                    {yGrid.map(c => {
                        if (c.name === BOTTLE_CELL) {
                            const v = selectedIndex >= 0 ? slots[selectedIndex] : 0;
                            const words = (bottleContents[v] ?? bottleContents[2]).words;
                            const around = BOTTLE_COUNT + 1;
                            return (
                                <button type="button" key={c.name} class={"dbg-btn" + (count > 0 ? " on" : "")} disabled={!ready}
                                        title={count + " of " + BOTTLE_COUNT + " bottles: click for " + (count + 1) % around +
                                            ", shift-click for " + (count + BOTTLE_COUNT) % around}
                                        onClick={e => bottles.stepCount(e.shiftKey ? -1 : 1)}>
                                    <Icon gfx={gfx} words={v !== 0 ? words : bottleContents[2].words} dim={count === 0}/>
                                </button>
                            );
                        }
                        const cur = c.current(get);
                        const next = (dir: number) => c.levels[(cur + dir + c.levels.length) % c.levels.length];
                        const shown = c.levels[cur === 0 ? 1 : cur];
                        return (
                            <button type="button" key={c.name} class={"dbg-btn" + (cur !== 0 ? " on" : "")} disabled={!ready}
                                    title={c.name + ": " + c.levels[cur].name + " (click for " + next(1).name + ")"}
                                    onClick={e => send(...next(e.shiftKey ? -1 : 1).ops)}>
                                {shown.words2
                                    ? <Both gfx={gfx} words={shown.words} words2={shown.words2}/>
                                    : <Icon gfx={gfx} words={shown.words} dim={cur === 0}/>}
                            </button>
                        );
                    })}
                </div>
                <div class="dbg-bottles">
                    {slots.map((v, i) => (
                        <button type="button" key={i} class={"dbg-btn" + (v !== 0 ? " on" : "") + (i === selectedIndex ? " selected" : "")}
                                disabled={!ready || v === 0}
                                title={v === 0 ? "Bottle " + (i + 1) + ": not available" : "Bottle " + (i + 1) + ": " + bottleContents[v]?.name +
                                    (i === selectedIndex ? " (selected)" : "") + ". Click for the next contents, shift-click for the previous"}
                                onClick={e => bottles.cycle(i, e.shiftKey ? -1 : 1)}>
                            <Icon gfx={gfx} words={(v !== 0 ? bottleContents[v] ?? bottleContents[2] : bottleContents[2]).words} dim={v === 0}/>
                        </button>
                    ))}
                </div>
            </div>
        </Section>
    );
}

// the value of no sword that the game uses, 0 or $FF, as last seen:
let swordNone = 0;

// Equipment is laid out as the item menu does: sword, shield and armor in a row.
function Equipment({gfx, get, send, ready}: Common) {
    return (
        <Section title="Equipment" note="Click to upgrade, wrapping around; shift-click to downgrade.">
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
                        <button type="button" key={e.name} class="dbg-btn on" disabled={!ready}
                                title={e.name + ": " + e.levels[cur].name + " (click for " + e.levels[(cur + 1) % n].name + ")"}
                                onClick={ev => to((cur + (ev.shiftKey ? n - 1 : 1)) % n)}>
                            <Icon gfx={gfx} words={e.levels[cur].words}/>
                        </button>
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
function Panel({items, gfx, get, send, ready}: Common & { items: Placed[] }) {
    return (
        <div class="dbg-panel" style={`width: ${PANEL_W * TILE}px; height: ${PANEL_H * TILE}px`}>
            {items.map(it => {
                const has = it.c.has(get);
                return (
                    <button type="button" key={it.c.name} class={"dbg-btn dbg-bare" + (has ? " on" : "")} disabled={!ready}
                            title={(has ? "Remove " : "Give ") + it.c.name}
                            style={`left: ${it.x * TILE - 3}px; top: ${it.y * TILE - 3}px`}
                            onClick={() => send(...(has ? it.c.take : it.c.give))}>
                        <Icon gfx={gfx} words={has || !it.missing ? it.c.words : it.missing} w={it.c.w} scale={PANEL_SCALE}
                              dim={!has && !it.missing}/>
                    </button>
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

function DungeonItems({gfx, get, send, ready, state}: Common & { state?: DebugState }) {
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
                {d >= 0 && dungeonItems(d).map(c => (
                    <ChoiceButton key={c.name} c={c} gfx={gfx} get={get} send={send} ready={ready}/>
                ))}
                <div class="dbg-btn dbg-static" title="Small keys of the dungeon Link is in">
                    <Icon gfx={gfx} words={SMALL_KEY_WORDS} w={1} scale={6}/>
                    <span class="dbg-count" title="Small keys of the dungeon Link is in">
                        {keys >= 0 && keys !== 0xFF ? keys : "-"}
                    </span>
                    <span class="dbg-step">
                        <button type="button" disabled={!ready || !inDungeon} title="Take a small key"
                                onClick={() => send({t: "keys", d: -1})}>−</button>
                        <button type="button" disabled={!ready || !inDungeon} title="Give a small key"
                                onClick={() => send({t: "keys", d: 1})}>+</button>
                    </span>
                </div>
            </div>
        </Section>
    );
}

function Hearts({gfx, get, send, ready}: Common) {
    const containers = get(0xF36C) >> 3;
    const pieces = get(0xF36B) & 3;
    const step = (c: number, p: number) => send({t: "hearts", c, p});
    return (
        <Section title="Heart Containers and Pieces">
            <div class="dbg-row">
                <div class="dbg-btn dbg-static">
                    <Icon gfx={gfx} words={HEART_FULL} w={1} scale={5}/>
                    <span class="dbg-count" title="Heart containers">{containers}</span>
                    <span class="dbg-step">
                        <button type="button" disabled={!ready || containers <= 1} title="Remove a heart container"
                                onClick={() => step(-1, 0)}>−</button>
                        <button type="button" disabled={!ready || containers >= 20} title="Give a heart container"
                                onClick={() => step(1, 0)}>+</button>
                    </span>
                </div>
                <div class="dbg-btn dbg-static">
                    <Icon gfx={gfx} words={HEART_HALF} w={1} scale={5}/>
                    <span class="dbg-count" title="Heart pieces, 4 make a container">{pieces}/4</span>
                    <span class="dbg-step">
                        <button type="button" disabled={!ready || (containers <= 1 && pieces === 0)}
                                title="Remove a heart piece" onClick={() => step(0, -1)}>−</button>
                        <button type="button" disabled={!ready || (containers >= 20 && pieces === 3)}
                                title="Give a heart piece" onClick={() => step(0, 1)}>+</button>
                    </span>
                </div>
            </div>
        </Section>
    );
}

function Fill({gfx, send, ready}: Common) {
    const fills: { w: string, name: string, words: number[], cols: number, scale: number }[] = [
        {w: "hearts", name: "Hearts", words: HEART_FULL, cols: 1, scale: 5},
        {w: "magic", name: "Magic", words: MAGIC_METER_WORDS, cols: 3, scale: 2},
        {w: "rupees", name: "Rupees (999)", words: RUPEE_WORDS, cols: 1, scale: 5},
        {w: "arrows", name: "Arrows", words: ARROW_WORDS, cols: 2, scale: 3},
        {w: "bombs", name: "Bombs", words: BOMB_WORDS, cols: 2, scale: 3},
    ];
    return (
        <Section title="Fill Up">
            <div class="dbg-grid">
                {fills.map(f => (
                    <button type="button" key={f.w} class="dbg-btn" disabled={!ready} title={"Fill up the " + f.name.toLowerCase()}
                            onClick={() => send({t: "fill", w: f.w})}>
                        <Icon gfx={gfx} words={f.words} w={f.cols} scale={f.scale}/>
                    </button>
                ))}
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

export default function DebugApp() {
    const {ch, lost, mainWrapper, vm} = useConnection();

    const created = vm.game?.isCreated || false;
    const state = vm["game/debug"] as DebugState | undefined;
    const {gfx, error} = useGfx(created);

    const sram = state?.sram;
    const get: Getter = addr => sram?.[addr - SRAM_FIRST] ?? 0;
    const send: Send = (...ops) => ch.command("game", "debug", {ops});
    const ready = created && !!state?.inGame;

    const p: Common = {gfx, get, send, ready};
    const bottles = useBottles(get, send);

    let status = "";
    if (!created) {
        status = "Select a game ROM on the main page first.";
    } else if (!state) {
        status = "Waiting for the game: connect the SNES and start the patched ROM.";
    } else if (!state.inGame) {
        status = "Link is not in the game right now (title, file select or a transition); the buttons wait for it.";
    }

    return (
        <Fragment>
            <div id="main-wrapper" ref={mainWrapper} class={lost ? "disabled" : ""}>
                <header>
                    <section class="rounded darken padded squeeze">
                        <h1>O2 Debug{created ? " - " + vm.game.gameName : ""}</h1>
                    </section>
                </header>
                <section class="squeeze">
                    <div class="flex-wrap">
                        <div class="content" style="width: 100%; display: flex">
                            <div style="flex: 1">
                                <a href="/">« Back to O2</a>
                            </div>
                            <div style="flex: 6; text-align: center">
                                {status
                                    ? <span style="color: #ffb000">{status}</span>
                                    : <span style="color: green">🟢 Connected to the game</span>}
                                {error && created && <div style="color: red; font-size: smaller">
                                    Graphics: {error}
                                </div>}
                            </div>
                        </div>
                        <div class="content" style="width: 100%">
                            <span class="dbg-note">
                                Changes are made by the game itself within a frame. Like anything the player does, they are shared
                                with the group by the sync settings on the main page.
                            </span>
                        </div>
                        <hr/>

                        <div class="dbg-columns">
                            <YGrid {...p} bottles={bottles}/>
                            <Equipment {...p}/>
                            <Abilities {...p}/>
                            <PendantsAndCrystals {...p}/>
                            <DungeonItems {...p} state={state}/>
                            <Hearts {...p}/>
                            <Fill {...p}/>
                            <Progress {...p}/>
                            <Enemies {...p}/>
                        </div>
                    </div>
                </section>
            </div>
            {lost && <ConnectionLost/>}
        </Fragment>
    );
}
