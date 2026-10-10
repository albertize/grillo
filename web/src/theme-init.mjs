// SPDX-License-Identifier: Apache-2.0
// Small independent entrypoint: apply preferences before the main React bundle.
import { readTheme, applyTheme } from './theme.mjs';
applyTheme(document, readTheme(() => window.localStorage), window.matchMedia('(prefers-color-scheme: dark)').matches);
