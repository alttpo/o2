import 'preact/debug';
import {Component, createRef, Fragment, h, render} from "preact";

// @ts-ignore
import ReactHintFactory from 'react-hint'
import {ViewModel} from './viewmodel';
import SNESView from "./snesview";
import ROMView, {PatchView} from "./romview";
import ServerView from "./serverview";
import GameView from "./gameview";
import DebugApp from "./games/alttp/debugview";
import {CommandHandler, ConnectionLost, useConnection} from "./connection";

const ReactHint = ReactHintFactory({Component, createElement: h, createRef: createRef})


export {CommandHandler};

export class TopLevelProps {
    ch: CommandHandler;
    vm: ViewModel;
}

const App = () => {
    const {ch, lost, mainWrapper, vm} = useConnection();

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
                            <SNESView ch={ch} vm={vm}/>
                        </div>

                        <div class="content" style={halfStyle}>
                            <ServerView ch={ch} vm={vm}/>
                        </div>

                        <div class="content" style={halfStyle}>
                            <ROMView ch={ch} vm={vm}/>
                        </div>

                        <div class="content" style={halfStyle}>
                            <PatchView ch={ch} vm={vm} patchError={patchError}/>
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
                                <GameView ch={ch} vm={vm}/>
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
            {lost && <ConnectionLost/>}
        </Fragment>
    );
}

document.addEventListener(
    "DOMContentLoaded",
    ev => {
        console.log("DOMContentLoaded");
        // the debug page is served by the same web app:
        const isDebugPage = window.location.pathname.replace(/\/+$/, "") === "/debug";
        render(isDebugPage ? <DebugApp/> : <App/>, document.querySelector('#app'));
    }
);
