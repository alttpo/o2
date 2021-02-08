import {ViewModel} from './viewmodel';
import {View, Views} from './views';

export class State {
    viewModel: ViewModel;

    constructor() {
        this.viewModel = {
            status: "",
            snes: {},
            server: {},
            rom: {},
            game: {}
        };
    }
}

interface ViewModelUpdate {
    v: string;
    m: object;
}

export class Host {
    private ws: WebSocket;
    private readonly viewModelObservers: { [viewModel: string]: View[] };

    state: State;
    views: Views;

    constructor(state: State, views: Views) {
        this.state = state;
        this.views = views;

        // map describing which views observe which view models:
        this.viewModelObservers = {
            'status': [],
            'snes': [this.views.snes]
        };
    }

    connect() {
        const {protocol, host} = window.location;
        const url = (protocol === "https:" ? "wss:" : "ws:") + "//" + host + "/ws/";

        this.ws = new WebSocket(url);
        this.ws.onmessage = this.onmessage.bind(this);
    }

    onmessage(e: MessageEvent<string>) {
        let msg = JSON.parse(e.data) as ViewModelUpdate;
        this.state.viewModel[msg.v] = msg.m;
        this.updateViewsObservingViewModel(msg.v);
    }

    bind(root: ParentNode) {
        for (let viewName in this.views) {
            if (!this.views.hasOwnProperty(viewName)) {
                continue;
            }

            let view = this.views[viewName];
            view.bind(root);
        }
    }

    private updateViewsObservingViewModel(viewModelName: string) {
        let observers = this.viewModelObservers[viewModelName];
        if (!observers) {
            return;
        }

        for (const view of observers) {
            view.render(this.state.viewModel);
        }
    }
}

document.addEventListener("DOMContentLoaded", ev => {
    console.log("DOMContentLoaded");
    let state = new State();
    let views = new Views();
    let host = new Host(state, views);
    host.bind(document);
    host.connect();
});
