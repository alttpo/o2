import {ViewModel} from "./viewmodel";
import {State} from "./index";

export interface View {
    // each root HTML element that is bound to this view:
    viewElements: NodeListOf<Element>;

    // the <template> that drives this view:
    templateElement: HTMLTemplateElement;

    // assigns viewElements and templateElement selected from the root node:
    bind(root: ParentNode): void;

    // renders each viewElement using the view model:
    render(): void;
}

abstract class AbstractView implements View {
    state: State;
    get model(): ViewModel {
        return this.state.viewModel;
    }

    viewElements: NodeListOf<Element>;
    templateElement: HTMLTemplateElement;

    abstract bind(root: ParentNode): void;

    abstract render(): void;
}

export class SNESView extends AbstractView {
    constructor(state: State) {
        super();
        this.state = state;
    }

    bind(root: ParentNode): void {
        this.templateElement = root.querySelector('#tmplSnes');
        this.viewElements = root.querySelectorAll(`[v-view=snes]`);
    }

    render(): void {
        for (let el of this.viewElements) {
            this.renderView(el);
        }
    }

    renderView(el: Element): void {
        // clear contents of destination element:
        while (el.firstChild) {
            el.removeChild(el.firstChild);
        }

        //
        this.templateElement.childNodes
    }
}

export class Views {
    [k: string]: View;
    snes: SNESView;

    constructor(state: State) {
        this.snes = new SNESView(state);
    }
}
