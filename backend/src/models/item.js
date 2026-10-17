/**
 * models/item.js
 * Mongoose schema for the Item document.
 * Kept deliberately minimal for Step 1.
 */

import mongoose from 'mongoose';

const itemSchema = new mongoose.Schema(
  {
    title: {
      type: String,
      required: [true, 'title is required'],
      trim: true,
      maxlength: [200, 'title must be 200 characters or fewer'],
    },
  },
  {
    // Automatically adds createdAt and updatedAt fields.
    timestamps: true,
  }
);

export const Item = mongoose.model('Item', itemSchema);
