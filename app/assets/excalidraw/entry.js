import React from 'react';
import ReactDOM from 'react-dom/client';
import {
  Excalidraw,
  exportToBlob,
  getSceneVersion,
  restoreElements,
  reconcileElements,
  CaptureUpdateAction,
} from '@excalidraw/excalidraw';

function bumpElementVersions(elements, existing) {
  var existingMap = new Map();
  for (var i = 0; i < existing.length; i++) {
    existingMap.set(existing[i].id, existing[i]);
  }
  return elements.map(function (el) {
    var existingEl = existingMap.get(el.id);
    if (existingEl && existingEl.version > el.version) {
      return Object.assign({}, el, { version: existingEl.version + 1 });
    }
    return el;
  });
}

export {
  React,
  ReactDOM,
  Excalidraw,
  exportToBlob,
  getSceneVersion,
  restoreElements,
  reconcileElements,
  CaptureUpdateAction,
  bumpElementVersions,
};
