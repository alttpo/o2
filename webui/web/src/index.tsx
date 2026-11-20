import 'preact/debug';
import {Component, createRef, Fragment, h, render} from "preact";
import {useEffect, useRef, useState} from "preact/hooks";

// @ts-ignore
import ReactHintFactory from 'react-hint'
import {ViewModel} from './viewmodel';
import SNESView from "./snesview";
import ROMView, {PatchView} from "./romview";
import ServerView from "./serverview";
import GameView from "./gameview";

const ReactHint = ReactHintFactory({Component, createElement: h, createRef: createRef})


interface ViewModelUpdate {
    v: string;
    m: any;
}

// RECONNECT_INTERVAL_MS is how often the web UI tries to reconnect to the o2 backend after losing the connection:
const RECONNECT_INTERVAL_MS = 1000;
// LOST_TIMEOUT_MS is how long the web UI waits for the connection to be established before it disables the page:
const LOST_TIMEOUT_MS = 10000;

export class CommandHandler {
    private ws: WebSocket | null = null;

    // setSocket changes the websocket that commands are sent to; the web UI reconnects with a new one:
    setSocket(ws: WebSocket | null) {
        this.ws = ws;
    }

    private send(data: string | Uint8Array) {
        if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
            console.log("command dropped: not connected to the o2 backend");
            return;
        }
        this.ws.send(data);
    }

    command(view: string, command: string, args: object) {
        console.log(`json command: ${view}.${command}`);
        this.send(JSON.stringify({
            v: view,
            c: command,
            a: args
        }));
    }

    binaryCommand(view: string, command: string, data: ArrayBuffer) {
        console.log(`binary command: ${view}.${command}`);

        const te = new TextEncoder();
        const dataArr = new Uint8Array(data);

        // encode view and command names as Pascal strings and append `data`:
        const buf = new Uint8Array(view.length + 1 + command.length + 1 + dataArr.length);
        let i = 0;
        buf[i++] = view.length;
        i += te.encodeInto(view, buf.subarray(i)).written;

        buf[i++] = command.length;
        i += te.encodeInto(command, buf.subarray(i)).written;

        buf.set(dataArr, i);

        this.send(buf);
    }
}

export class TopLevelProps {
    ch: CommandHandler;
    vm: ViewModel;
}

const App = () => {
    type TabName = "snes" | "rom" | "server" | "game";

    const ch = useRef<CommandHandler>(new CommandHandler());
    // lost is set when the websocket to the o2 backend could not be (re-)established within LOST_TIMEOUT_MS:
    const [lost, setLost] = useState(false);
    const mainWrapper = useRef<HTMLDivElement>(null);
    const [viewModel, setViewModel] = useState<ViewModel>({
        status: "",
        snes: {
            drivers: [], isConnected: false
        },
        rom: {
            isLoaded: false, name: "", title: "", region: "", version: "", folder: "", filename: ""
        },
        server: {
            isConnected: false, hostName: "", groupName: "", playerName: "", team: 0
        },
        game: {
            isCreated: false,
            gameName: ""
        }
    });

    useEffect(() => {
        const {protocol, host} = window.location;
        const url = `${protocol === "https:" ? "wss:" : "ws:"}//${host}/ws/`;

        let ws: WebSocket | null = null;
        let stopped = false;
        let reconnectTimer: number | undefined;
        let lostTimer: number | undefined;

        // disable the page if no connection is established in time:
        const startLostTimer = () => {
            if (lostTimer === undefined) {
                lostTimer = window.setTimeout(() => setLost(true), LOST_TIMEOUT_MS);
            }
        };

        const connect = () => {
            reconnectTimer = undefined;
            if (stopped) return;

            console.log("connect");
            const sock = new WebSocket(url);
            ws = sock;

            sock.onopen = () => {
                console.log("connected");
                window.clearTimeout(lostTimer);
                lostTimer = undefined;
                ch.current.setSocket(sock);
                setLost(false);
            };
            sock.onmessage = (e: MessageEvent<string>) => {
                const msg = JSON.parse(e.data) as ViewModelUpdate;
                setViewModel(vm => ({...vm, [msg.v]: msg.m}));
            };
            sock.onclose = () => {
                if (ws !== sock) return;
                console.log("disconnected");
                ws = null;
                ch.current.setSocket(null);
                if (stopped) return;
                startLostTimer();
                reconnectTimer = window.setTimeout(connect, RECONNECT_INTERVAL_MS);
            };
            // onclose always follows onerror, which reconnects.
        };

        startLostTimer();
        connect();

        return () => {
            stopped = true;
            window.clearTimeout(reconnectTimer);
            window.clearTimeout(lostTimer);
            ws?.close();
        };
    }, []);

    // keep keyboard focus and input out of the page while it is disabled:
    useEffect(() => {
        if (!mainWrapper.current) return;
        if (lost) {
            mainWrapper.current.setAttribute("inert", "");
        } else {
            mainWrapper.current.removeAttribute("inert");
        }
    }, [lost]);

    const vm = viewModel;

    // two sections per row, which shrink instead of being widened by their controls:
    const halfStyle = "flex: 0 0 50%; box-sizing: border-box; min-width: 0";
    const snesGo = vm.snes?.isConnected || false;
    const romGo = vm.game?.isCreated || false;
    const romStatus = romGo ? "" : "select a game ROM";

    // the patch version can only be checked once the SNES is a GO; until then, or while waiting for the SNES to
    // respond, there is nothing useful to report:
    const patchGo = (snesGo && vm.game?.isCreated && vm.game?.romVerified) || false;
    const rawPatchStatus = vm.game?.romStatus || "";
    const patchError = (snesGo && vm.game?.isCreated && !patchGo
        && !rawPatchStatus.startsWith("waiting to read")
        && !rawPatchStatus.includes("no response from the SNES")) ? rawPatchStatus : "";

    return (
        <Fragment>
            <div id="main-wrapper" ref={mainWrapper} class={lost ? "disabled" : ""}>
                <header>
                    <section class="rounded darken padded squeeze">
                        <h1>O2{ (vm.o2?.version) ? " " + vm.o2?.version : ""}{
                            (vm.game?.isCreated || false) ? " - " + vm.game.gameName : ""
                        }</h1>
                    </section>
                </header>
                <section class="squeeze">
                    <div class="flex-wrap">
                        <div class="content" style={halfStyle}>
                            <SNESView ch={ch.current} vm={vm}/>
                        </div>

                        <div class="content" style={halfStyle}>
                            <ServerView ch={ch.current} vm={vm}/>
                        </div>

                        <div class="content" style={halfStyle}>
                            <ROMView ch={ch.current} vm={vm}/>
                        </div>

                        <div class="content" style={halfStyle}>
                            <PatchView ch={ch.current} vm={vm} patchError={patchError}/>
                        </div>

                        <div class="content" style="width: 100%; display: flex">
                            <div style="flex: 1 0 0%">
                                <span>SNES:&nbsp;</span>{snesGo ? <span style="color: green">🟢 GO</span> : <span style="color: red">⭕ NO GO</span>}
                            </div>
                            <div style="flex: 1 0 0%">
                                <span>Server:&nbsp;</span>{vm.server?.isConnected ? <span style="color: green">🟢 GO</span> : <span style="color: red">⭕ NO GO</span>}
                            </div>
                            <div style="flex: 1 0 0%" title={romStatus}>
                                <span>ROM:&nbsp;</span>{romGo ? <span style="color: green">🟢 GO</span> : <span style="color: red">⭕ NO GO</span>}
                                {!romGo && romStatus && <div style="color: red; font-size: smaller">{romStatus}</div>}
                            </div>
                            <div style="flex: 1 0 0%" title={patchError}>
                                <span>Patch:&nbsp;</span>{patchGo ? <span style="color: green">🟢 GO</span> : <span style="color: red">⭕ NO GO</span>}
                            </div>
                            <div style="flex: 0 0 15%">
                                <a href="/log.txt" style="float: right">Download Logs</a>
                            </div>
                        </div>

                        <hr/>

                        {vm.game?.isCreated && (
                            <div class="content flex-1">
                                <GameView ch={ch.current} vm={vm}/>
                            </div>
                        )}
                    </div>
                </section>
                <ReactHint autoPosition events/>
                {/* tooltips with a link in them, which stay open while the mouse is over them so it can be clicked: */}
                <ReactHint autoPosition events persist
                           attribute="data-rhl"
                           onRenderContent={(target: HTMLElement, content: string) => (
                               <div class="react-hint__content">
                                   {content} <a href={target.getAttribute('data-rhl-href')}
                                                target="_blank">{target.getAttribute('data-rhl-link')}</a>.
                               </div>
                           )}/>
            </div>
            {lost && (
                <div class="connection-lost-backdrop">
                    <div class="connection-lost rounded darken padded" role="alertdialog" aria-modal="true"
                         aria-labelledby="connection-lost-title">
                        <h3 id="connection-lost-title">⭕ Lost connection to the O2 backend</h3>
                        <p>
                            This page could not reach O2 for the last {LOST_TIMEOUT_MS / 1000} seconds. O2 may have
                            been closed or may have crashed; the page is disabled until the connection is back.
                        </p>
                        <p>
                            Start O2 again if it is not running. This page keeps trying to reconnect and comes back
                            on its own once O2 is running.
                        </p>
                        <p class="connection-lost-status">Reconnecting…</p>
                    </div>
                </div>
            )}
        </Fragment>
    );
}

document.addEventListener(
    "DOMContentLoaded",
    ev => {
        console.log("DOMContentLoaded");
        render(<App/>, document.querySelector('#app'));
    }
);
