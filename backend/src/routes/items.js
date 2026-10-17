/**
 * routes/items.js
 * Express router for the /api/items resource.
 *
 * GET  /api/items  - return all items, newest first.
 * POST /api/items  - create a new item.
 *
 * Log lines emitted here automatically include trace_id and span_id
 * because the OTel Express instrumentation has already set an active span
 * by the time these handlers run, and logger.js reads it from the context.
 */

import { Router } from 'express';
import { Item } from '../models/item.js';
import { logger } from '../logger.js';

const router = Router();

// GET /api/items
router.get('/', async (req, res, next) => {
  try {
    const items = await Item.find().sort({ createdAt: -1 }).lean();
    logger.info('items listed', { count: items.length });
    res.json({ items });
  } catch (err) {
    next(err);
  }
});

// POST /api/items
router.post('/', async (req, res, next) => {
  try {
    const { title } = req.body;

    if (!title || typeof title !== 'string' || title.trim() === '') {
      const err = new Error('title is required and must be a non-empty string');
      err.status = 400;
      return next(err);
    }

    const item = await Item.create({ title: title.trim() });
    logger.info('item created', { item_id: item._id.toString() });
    res.status(201).json({ item });
  } catch (err) {
    next(err);
  }
});

export default router;
