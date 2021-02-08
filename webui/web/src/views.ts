import {ViewModel} from "./viewmodel";

export interface View {
    // each root HTML element that is bound to this view:
    viewElements: NodeListOf<Element>;

    // the <template> that drives this view:
    templateElement: HTMLTemplateElement;

    // assigns viewElements and templateElement selected from the root node:
    bind(root: ParentNode): void;

    // renders each viewElement using the view model:
    render(vm: ViewModel): void;
}

abstract class AbstractView implements View {
    viewElements: NodeListOf<Element>;

    templateElement: HTMLTemplateElement;

    cloneTemplate(el: Element): void {
        let cloned = this.templateElement.content.cloneNode(true);
        while (el.firstChild) {
            el.removeChild(el.firstChild);
        }
        el.appendChild(cloned);
    }

    abstract bind(root: ParentNode): void;

    abstract render(vm: ViewModel): void;
}

export class SNESView extends AbstractView {
    bind(root: ParentNode): void {
        this.templateElement = root.querySelector('#tmplSnes');
        this.viewElements = root.querySelectorAll(`[v-view=snes]`);
    }

    render(vm: ViewModel): void {
        for (let el of this.viewElements) {
            this.cloneTemplate(el);
        }
    }
}

export class Views {
    [k: string]: View;
    snes: SNESView;

    constructor() {
        this.snes = new SNESView();
    }
}
