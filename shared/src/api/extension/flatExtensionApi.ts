import { SettingsCascade } from '../../settings/settings'
import { Remote, proxy } from 'comlink'
import * as sourcegraph from 'sourcegraph'
import { BehaviorSubject, Subject, ReplaySubject, of, Observable, from } from 'rxjs'
import { FlatExtHostAPI, MainThreadAPI } from '../contract'
import { syncSubscription } from '../util'
import { switchMap, mergeMap, map } from 'rxjs/operators'
import { proxySubscribable, providerResultToObservable } from './api/common'
import { TextDocumentIdentifier, match } from '../client/types/textDocument'
import { getModeFromPath } from '../../languages'
import { parseRepoURI } from '../../util/url'
import { ExtensionDocuments } from './api/documents'
import { toPosition } from './api/types'
import { TextDocumentPositionParams } from '../protocol'
import { ProvideTextDocumentHoverSignature, getHover } from './hover'

/**
 * Holds the entire state exposed to the extension host
 * as a single plain object
 */
export interface ExtState {
    settings: Readonly<SettingsCascade<object>>

    // Workspace
    roots: readonly sourcegraph.WorkspaceRoot[]
    versionContext: string | undefined

    // Search
    queryTransformers: sourcegraph.QueryTransformer[]

    // Lang
    hoverProviders: RegisteredHoverProvider[]
}

interface RegisteredHoverProvider {
    selector: sourcegraph.DocumentSelector
    provider: sourcegraph.HoverProvider
}

export interface InitResult {
    configuration: sourcegraph.ConfigurationService
    workspace: PartialWorkspaceNamespace
    exposedToMain: FlatExtHostAPI
    // todo this is needed as a temp solution for getter problem
    state: Readonly<ExtState>
    commands: typeof sourcegraph['commands']
    search: typeof sourcegraph['search']
    languages: Pick<typeof sourcegraph['languages'], 'registerHoverProvider'>
}

/**
 * mimics sourcegraph.workspace namespace without documents
 */
export type PartialWorkspaceNamespace = Omit<
    typeof sourcegraph['workspace'],
    'textDocuments' | 'onDidOpenTextDocument' | 'openedTextDocuments' | 'roots' | 'versionContext'
>
/**
 * Holds internally ExtState and manages communication with the Client
 * Returns the initialized public extension API pieces ready for consumption and the internal extension host API ready to be exposed to the main thread
 * NOTE that this function will slowly merge with the one in extensionHost.ts
 *
 * @param mainAPI
 */
export const initNewExtensionAPI = (
    mainAPI: Remote<MainThreadAPI>,
    initialSettings: Readonly<SettingsCascade<object>>,
    textDcuments: ExtensionDocuments
): InitResult => {
    const state: ExtState = {
        roots: [],
        versionContext: undefined,
        settings: initialSettings,
        queryTransformers: [],
        hoverProviders: [],
    }

    const configChanges = new BehaviorSubject<void>(undefined)
    // Most extensions never call `configuration.get()` synchronously in `activate()` to get
    // the initial settings data, and instead only subscribe to configuration changes.
    // In order for these extensions to be able to access settings, make sure `configuration` emits on subscription.

    const hoverProvidersChanges = new BehaviorSubject<RegisteredHoverProvider[]>([])

    const rootChanges = new Subject<void>()
    const queryTransformersChanges = new ReplaySubject<sourcegraph.QueryTransformer[]>(1)
    queryTransformersChanges.next([])

    const versionContextChanges = new Subject<string | undefined>()

    const exposedToMain: FlatExtHostAPI = {
        // Configuration
        syncSettingsData: data => {
            state.settings = Object.freeze(data)
            configChanges.next()
        },

        // Workspace
        syncRoots: (roots): void => {
            state.roots = Object.freeze(roots.map(plain => ({ ...plain, uri: new URL(plain.uri) })))
            rootChanges.next()
        },
        syncVersionContext: context => {
            state.versionContext = context
            versionContextChanges.next(context)
        },

        // Search
        transformSearchQuery: query =>
            // TODO (simon) I don't enjoy the dark arts below
            // we return observable because of potential deferred addition of transformers
            // in this case we need to reissue the transformation and emit the resulting value
            // we probably won't need an Observable if we somehow coordinate with extensions activation
            proxySubscribable(
                queryTransformersChanges.pipe(
                    switchMap(transformers =>
                        transformers.reduce(
                            (currentQuery: Observable<string>, transformer) =>
                                currentQuery.pipe(
                                    mergeMap(query => {
                                        const result = transformer.transformQuery(query)
                                        return result instanceof Promise ? from(result) : of(result)
                                    })
                                ),
                            of(query)
                        )
                    )
                )
            ),

        // Language
        getHover: (textParameters: TextDocumentPositionParams) => {
            const document = textDcuments.get(textParameters.textDocument.uri)

            const matchedProviders = hoverProvidersChanges.pipe(
                map(providers =>
                    providersForDocument(textParameters.textDocument, providers, ({ selector }) => selector).map(
                        ({ provider }): ProvideTextDocumentHoverSignature => parameters =>
                            providerResultToObservable(provider.provideHover(document, toPosition(parameters.position)))
                    )
                )
            )
            return proxySubscribable(getHover(matchedProviders, textParameters))
        },
    }

    // Configuration
    const getConfiguration = <C extends object>(): sourcegraph.Configuration<C> => {
        const snapshot = state.settings.final as Readonly<C>

        const configuration: sourcegraph.Configuration<C> & { toJSON: any } = {
            value: snapshot,
            get: key => snapshot[key],
            update: (key, value) => mainAPI.applySettingsEdit({ path: [key as string | number], value }),
            toJSON: () => snapshot,
        }
        return configuration
    }

    // Workspace
    const workspace: PartialWorkspaceNamespace = {
        onDidChangeRoots: rootChanges.asObservable(),
        rootChanges: rootChanges.asObservable(),
        versionContextChanges: versionContextChanges.asObservable(),
    }

    // Commands
    const commands: typeof sourcegraph['commands'] = {
        executeCommand: (command, ...args) => mainAPI.executeCommand(command, args),
        registerCommand: (command, callback) => syncSubscription(mainAPI.registerCommand(command, proxy(callback))),
    }

    // Search
    const search: typeof sourcegraph['search'] = {
        registerQueryTransformer: transformer =>
            addElementWithRollback(transformer, {
                get: () => state.queryTransformers,
                set: values => (state.queryTransformers = values),
                notifyWith: queryTransformersChanges,
            }),
    }

    // Languages
    const registerHoverProvider = (
        selector: sourcegraph.DocumentSelector,
        provider: sourcegraph.HoverProvider
    ): sourcegraph.Unsubscribable =>
        addElementWithRollback(
            { provider, selector },
            {
                get: () => state.hoverProviders,
                set: values => (state.hoverProviders = values),
                notifyWith: hoverProvidersChanges,
            }
        )

    return {
        configuration: Object.assign(configChanges.asObservable(), {
            get: getConfiguration,
        }),
        exposedToMain,
        workspace,
        state,
        commands,
        search,
        languages: {
            registerHoverProvider,
        },
    }
}

// TODO probably worth separate test suit.home
// maybe copy from registry.ts?
function providersForDocument<P>(
    document: TextDocumentIdentifier,
    entries: P[],
    selector: (p: P) => sourcegraph.DocumentSelector
): P[] {
    return entries.filter(provider =>
        match(selector(provider), {
            uri: document.uri,
            languageId: getModeFromPath(parseRepoURI(document.uri).filePath || ''),
        })
    )
}

/**
 * adds an element to an array with the ability to remove it back via Unsubscribable.
 * Both of these changes will be notified via "notifyWith" subject
 *
 * @returns Unsubscribable to remove the element from the array.
 */
function addElementWithRollback<T>(
    value: T,
    {
        get,
        set,
        notifyWith: notify,
    }: {
        get: () => T[]
        set: (val: T[]) => void
        notifyWith: Subject<T[]>
    }
): sourcegraph.Unsubscribable {
    const modifiedArray = get().concat(value)
    set(modifiedArray)
    notify.next(modifiedArray)
    return {
        unsubscribe: () => {
            // eslint-disable-next-line id-length
            const filtered = get().filter(t => t !== value)
            set(modifiedArray)
            notify.next(filtered)
        },
    }
}
