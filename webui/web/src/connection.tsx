import {useEffect, useRef, useState} from "preact/hooks";
import {h} from "preact";
import {ViewModel} from "./viewmodel";

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

// useConnection connects to the o2 backend over a websocket, which keeps the view models up to date and sends the
// page's commands. `lost` is set when the connection could not be established in time, and `mainWrapper` must be
// the ref of the element holding the page, which is disabled while `lost`:
export function useConnection() {
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

    return {ch: ch.current, lost, mainWrapper, vm: viewModel};
}

// ConnectionLost covers the page while the connection to the o2 backend is lost:
export function ConnectionLost() {
    return (
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
    );
}
