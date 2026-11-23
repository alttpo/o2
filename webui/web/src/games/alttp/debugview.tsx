// The /debug page: switches debug mode, in which the alttp game view on the main page can give and take away items,
// fill up the player, set the progression, kill enemies and warp (see debugsections.tsx), and shows what o2 sends.

import {Fragment, h} from "preact";
import {ConnectionLost, useConnection} from "../../connection";
import {GameALTTPViewModel} from "../../viewmodel";
import {Section} from "./debugsections";

// the "game/bandwidth" view model: what o2 sent in the last second, per game message type (see bandwidth.go):
interface BandwidthRow {
    name: string;
    // messages per second, their bytes per second, and with their share of the packets' fixed cost:
    msgs: number;
    bytes: number;
    wire: number;
}

interface BandwidthState {
    packets: number;
    sectorPackets: number;
    wire: number;
    rows: BandwidthRow[] | null;
}

// Bandwidth shows what o2 sends, per message type. Wire bytes include each message's share of its packet's envelope
// and IPv4/UDP headers.
function Bandwidth({bw}: { bw?: BandwidthState }) {
    const kib = (b: number) => (b / 1024).toFixed(2);
    return (
        <Section title="Bandwidth sent"
                 note="Last second, per message type. Wire includes each message's share of its packet's headers.">
            {!bw ? <div class="dbg-note">Nothing sent yet.</div> : (
                <Fragment>
                    <div>
                        {bw.packets.toFixed(1)} packets/s ({bw.sectorPackets.toFixed(1)} to the sector), {kib(bw.wire)} KiB/s
                    </div>
                    <table class="dbg-bandwidth">
                        <thead>
                        <tr>
                            <th style="text-align: left">message</th>
                            <th>msgs/s</th>
                            <th>B/s</th>
                            <th>wire KiB/s</th>
                        </tr>
                        </thead>
                        <tbody>
                        {(bw.rows ?? []).map(r => (
                            <tr key={r.name}>
                                <td style="text-align: left">{r.name}</td>
                                <td style="text-align: right">{r.msgs.toFixed(1)}</td>
                                <td style="text-align: right">{r.bytes.toFixed(0)}</td>
                                <td style="text-align: right">{kib(r.wire)}</td>
                            </tr>
                        ))}
                        </tbody>
                    </table>
                </Fragment>
            )}
        </Section>
    );
}

export default function DebugApp() {
    const {ch, lost, mainWrapper, vm} = useConnection();

    const created = vm.game?.isCreated || false;
    const alttp = created && vm.game.gameName === "ALTTP";
    const game = vm.game as GameALTTPViewModel;
    const debugMode = alttp && !!game.debugMode;
    const bw = vm["game/bandwidth"] as BandwidthState | undefined;

    let status = "";
    if (!created) {
        status = "Select a game ROM on the main page first.";
    } else if (!alttp) {
        status = "Debug mode is only for A Link to the Past.";
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
                                {status && <span style="color: #ffb000">{status}</span>}
                            </div>
                        </div>
                        <hr/>

                        <div class="dbg-columns">
                            <Section title="Debug Mode"
                                     note="In debug mode the game view on the main page changes what the player has when its items, equipment and hearts are clicked, and shows the progression, enemies and warp tools. Otherwise it only shows what the player has.">
                                <label for="dbg-mode">
                                    <input type="checkbox" id="dbg-mode" checked={debugMode} disabled={!alttp}
                                           onChange={e => ch.command("game", "setField",
                                               {debugMode: (e.target as HTMLInputElement).checked})}/>
                                    Debug mode
                                </label>
                            </Section>
                            <Bandwidth bw={bw}/>
                        </div>
                    </div>
                </section>
            </div>
            {lost && <ConnectionLost/>}
        </Fragment>
    );
}
