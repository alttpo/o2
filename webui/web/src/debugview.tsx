// The /debug page: tools to give and take away items, fill up the player and kill enemies while debugging.
// Each icon is drawn from the game's own graphics (see debuggfx.tsx).

import {Fragment, h} from "preact";
import {useState} from "preact/hooks";
import {ConnectionLost, useConnection} from "./connection";
import {DebugOp, Gfx, Icon, useGfx} from "./debuggfx";
import {
    abilities, ARROW_WORDS, BOMB_WORDS, BOTTLE_ADDR, BOTTLE_COUNT, BOTTLE_SELECTED, bottleContents,
    Choice, crystals, dungeonItems, dungeons, equipment, Getter, halfMagic, HEART_FULL, HEART_HALF,
    MAGIC_METER_WORDS, mapMarkers, pendants, progressFlags, progressLevels, RUPEE_WORDS, SMALL_KEY_LIVE,
    SMALL_KEY_WORDS, SRAM_FIRST, yItems
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

// ChoiceButton gives the item if the player doesn't have it, and takes it away if they do.
function ChoiceButton({c, gfx, get, send, ready, scale = 3}: Common & { c: Choice, scale?: number }) {
    const has = c.has(get);
    return (
        <button type="button" class={"dbg-btn" + (has ? " on" : "")} disabled={!ready}
                title={(has ? "Remove " : "Give ") + c.name}
                onClick={() => send(...(has ? c.take : c.give))}>
            <Icon gfx={gfx} words={c.words} w={c.w} scale={scale} dim={!has}/>
            <span class="dbg-label">{c.name}</span>
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

function YItems(p: Common) {
    return (
        <Section title="Y Items">
            <div class="dbg-grid">
                {yItems.map(c => <ChoiceButton key={c.name} c={c} {...p}/>)}
            </div>
        </Section>
    );
}

function Bottles({gfx, get, send, ready}: Common) {
    const slots = Array.from({length: BOTTLE_COUNT}, (_, i) => get(BOTTLE_ADDR + i));
    const selected = get(BOTTLE_SELECTED);

    // the bottle selected must hold something, or be 0 without any bottle:
    const select = (after: number[]): DebugOp => {
        const first = after.findIndex(v => v !== 0);
        const cur = selected - 1;
        const ok = cur >= 0 && cur < BOTTLE_COUNT && after[cur] !== 0;
        return {t: "set", a: BOTTLE_SELECTED, v: ok ? selected : first + 1};
    };

    const give = (content: number) => {
        const slot = slots.findIndex(v => v === 0);
        if (slot < 0) return;
        const after = slots.slice();
        after[slot] = content;
        send({t: "set", a: BOTTLE_ADDR + slot, v: content}, select(after));
    };
    const take = (slot: number) => {
        const after = slots.slice();
        after[slot] = 0;
        send({t: "set", a: BOTTLE_ADDR + slot, v: 0}, select(after));
    };
    const full = slots.every(v => v !== 0);

    return (
        <Section title="Bottles" note="Click a bottle to empty its slot; click a content to put it in the first free slot.">
            <div class="dbg-grid">
                {slots.map((v, i) => (
                    <button type="button" key={i} class={"dbg-btn" + (v !== 0 ? " on" : "")}
                            disabled={!ready || v === 0}
                            title={v === 0 ? "Free slot" : "Remove the " + bottleContents[v]?.name}
                            onClick={() => take(i)}>
                        <Icon gfx={gfx} words={(bottleContents[v] ?? bottleContents[2]).words} dim={v === 0}/>
                        <span class="dbg-label">{"Slot " + (i + 1) + (selected === i + 1 && v !== 0 ? " ▶" : "")}</span>
                    </button>
                ))}
            </div>
            <div class="dbg-grid dbg-sub">
                {bottleContents.slice(1).map((c, i) => (
                    <button type="button" key={i} class="dbg-btn" disabled={!ready || full}
                            title={"Put a " + c.name + " in a free bottle"}
                            onClick={() => give(i + 1)}>
                        <Icon gfx={gfx} words={c.words} scale={2}/>
                        <span class="dbg-label">{c.name}</span>
                    </button>
                ))}
            </div>
        </Section>
    );
}

function Equipment({gfx, get, send, ready}: Common) {
    return (
        <Section title="Equipment" note="Click a level, or step up and down.">
            {equipment.map(e => {
                const v = get(e.addr);
                // the smithy sets the sword to $FF while it is being tempered:
                const cur = v < e.levels.length ? v : 0;
                const to = (level: number) => send({t: "set", a: e.addr, v: level}, {t: "gfx", w: e.gfx});
                return (
                    <div key={e.name} class="dbg-row">
                        <span class="dbg-row-label">{e.name}</span>
                        <button type="button" disabled={!ready || cur === 0} title={"Downgrade the " + e.name.toLowerCase()}
                                onClick={() => to(cur - 1)}>▼
                        </button>
                        {e.levels.map((l, i) => (
                            <button type="button" key={i} class={"dbg-btn" + (i === cur ? " on" : "")}
                                    disabled={!ready} title={l.name} onClick={() => to(i)}>
                                <Icon gfx={gfx} words={l.words} dim={i !== cur}/>
                                <span class="dbg-label">{l.name}</span>
                            </button>
                        ))}
                        <button type="button" disabled={!ready || cur === e.levels.length - 1}
                                title={"Upgrade the " + e.name.toLowerCase()}
                                onClick={() => to(cur + 1)}>▲
                        </button>
                    </div>
                );
            })}
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

function PendantsAndCrystals(p: Common) {
    return (
        <Section title="Pendants and Crystals">
            <div class="dbg-grid">
                {pendants.map(c => <ChoiceButton key={c.name} c={c} {...p}/>)}
            </div>
            <div class="dbg-grid dbg-sub">
                {crystals.map(c => <ChoiceButton key={c.name} c={c} {...p} scale={4}/>)}
            </div>
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
                    <span class="dbg-label">
                        Small keys: {keys >= 0 && keys !== 0xFF ? keys : "-"}
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
                    <span class="dbg-label">Containers: {containers}</span>
                    <span class="dbg-step">
                        <button type="button" disabled={!ready || containers <= 1} title="Remove a heart container"
                                onClick={() => step(-1, 0)}>−</button>
                        <button type="button" disabled={!ready || containers >= 20} title="Give a heart container"
                                onClick={() => step(1, 0)}>+</button>
                    </span>
                </div>
                <div class="dbg-btn dbg-static">
                    <Icon gfx={gfx} words={HEART_HALF} w={1} scale={5}/>
                    <span class="dbg-label">Pieces: {pieces} of 4</span>
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
                        <span class="dbg-label">{f.name}</span>
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
                            title={"Set the progress to " + n} onClick={() => send({t: "set", a: 0xF3C5, v: i})}>
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
                            <YItems {...p}/>
                            <Bottles {...p}/>
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
