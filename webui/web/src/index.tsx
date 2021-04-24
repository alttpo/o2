import 'preact/debug';
import {Component, createRef, Fragment, h, render} from "preact";
import {useEffect, useRef, useState} from "preact/hooks";

// @ts-ignore
import ReactHintFactory from 'react-hint'
import {ViewModel} from './viewmodel';
import SNESView from "./snesview";
import ROMView from "./romview";
import ServerView from "./serverview";
import GameView from "./gameview";
import {JSXInternal} from "preact/src/jsx";

const ReactHint = ReactHintFactory({Component, createElement: h, createRef: createRef})


interface ViewModelUpdate {
    v: string;
    m: any;
}

export class CommandHandler {
    private ws: WebSocket;

    constructor(ws: WebSocket) {
        this.ws = ws;
    }

    command(view: string, command: string, args: object) {
        console.log(`json command: ${view}.${command}`);
        this.ws.send(JSON.stringify({
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

        this.ws.send(buf);
    }
}

export class TopLevelProps {
    ch: CommandHandler;
    vm: ViewModel;
}

const App = () => {
    type TabName = "snes" | "rom" | "server" | "game";

    const ws = useRef<WebSocket>(null);
    const ch = useRef<CommandHandler>(null);
    const [viewModel, setViewModel] = useState<ViewModel>({
        status: "",
        snes: {
            drivers: [], isConnected: false
        },
        rom: {
            isLoaded: false, region: "", name: "", title: "", version: ""
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

        console.log("connect");
        ws.current = new WebSocket(url);
        ch.current = new CommandHandler(ws.current);

        return () => {
            ws.current.close();
        };
    }, []);

    useEffect(() => {
        if (!ws.current) return;

        ws.current.onmessage = (e: MessageEvent<string>) => {
            let msg = JSON.parse(e.data) as ViewModelUpdate;
            setViewModel(vm => ({...vm, [msg.v]: msg.m}));
        };
    }, [viewModel]);

    const vm = viewModel;

    return (
        <Fragment>
            <div id="main-wrapper">
                <header>
                    <section class="rounded darken padded squeeze">
                        <h1>O2{
                            (vm.game?.isCreated || false) ? " - " + vm.game.gameName : ""
                        }</h1>
                    </section>
                </header>
                <section class="squeeze">
                    <div class="flex-wrap">
                        <div class="content flex-1">
                            <SNESView ch={ch.current} vm={vm}/>
                        </div>

                        <div class="content flex-1">
                            <ROMView ch={ch.current} vm={vm}/>
                        </div>

                        <div class="content flex-1">
                            <ServerView ch={ch.current} vm={vm}/>
                        </div>

                        {vm.game?.isCreated && (
                            <div class="content flex-1">
                                <GameView ch={ch.current} vm={vm}/>
                            </div>
                        )}
                    </div>
                </section>
                <ReactHint autoPosition events/>
            </div>
            <footer>
                <section class="rounded darken padded-lr squeeze">
                    <span>{vm.status}</span>
                    <span style="float:right">
                        <a href="/log.txt">Download Logs</a>
                    </span>
                </section>
            </footer>
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
