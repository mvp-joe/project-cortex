use tokenizers::Tokenizer;
use tract_onnx::prelude::*;
use rayon::prelude::*;
use std::ffi::CStr;
use std::os::raw::c_char;
use std::sync::Arc;

pub struct EmbeddingsHandle {
    tokenizer: Tokenizer,
    model: Arc<SimplePlan<TypedFact, Box<dyn TypedOp>, Graph<TypedFact, Box<dyn TypedOp>>>>,
    pool: rayon::ThreadPool,
    embedding_dim: usize,
}

/// Initialize embeddings model
/// Returns NULL on error
#[no_mangle]
pub extern "C" fn embeddings_init(
    model_path: *const c_char,
    tokenizer_path: *const c_char,
) -> *mut EmbeddingsHandle {
    if model_path.is_null() || tokenizer_path.is_null() {
        return std::ptr::null_mut();
    }

    let model_path = unsafe { CStr::from_ptr(model_path) };
    let tokenizer_path = unsafe { CStr::from_ptr(tokenizer_path) };

    let model_path = match model_path.to_str() {
        Ok(s) => s,
        Err(_) => return std::ptr::null_mut(),
    };
    let tokenizer_path = match tokenizer_path.to_str() {
        Ok(s) => s,
        Err(_) => return std::ptr::null_mut(),
    };

    // Load tokenizer
    let tokenizer = match Tokenizer::from_file(tokenizer_path) {
        Ok(t) => t,
        Err(e) => {
            eprintln!("[RAYON] Failed to load tokenizer: {}", e);
            return std::ptr::null_mut();
        }
    };

    // Load ONNX model with tract
    let model = match tract_onnx::onnx()
        .model_for_path(model_path)
        .and_then(|m| m.into_optimized())
        .and_then(|m| m.into_runnable()) {
        Ok(m) => m,
        Err(e) => {
            eprintln!("[RAYON] Failed to load ONNX model: {}", e);
            return std::ptr::null_mut();
        }
    };

    // Get embedding dimension from model output (BGE-small default: 384)
    let embedding_dim = 384;

    // Create thread pool (fixed at 2 threads to prevent thermal throttling on M-series Macs)
    let num_threads = 2;
    let pool = match rayon::ThreadPoolBuilder::new()
        .num_threads(num_threads)
        .build() {
        Ok(p) => p,
        Err(e) => {
            eprintln!("[RAYON] Failed to create thread pool: {}", e);
            return std::ptr::null_mut();
        }
    };

    eprintln!("[RAYON] Model loaded successfully, using {} threads for parallel processing", num_threads);

    let handle = Box::new(EmbeddingsHandle {
        tokenizer,
        model: Arc::new(model),
        pool,
        embedding_dim,
    });

    Box::into_raw(handle)
}

/// Normalize a vector to unit length (L2 normalization)
fn normalize_vector(vec: &mut [f32]) {
    let norm: f32 = vec.iter()
        .map(|x| x * x)
        .sum::<f32>()
        .sqrt();

    if norm > 1e-12 {  // Avoid division by zero
        for val in vec.iter_mut() {
            *val /= norm;
        }
    }
}

/// Encode text to normalized embeddings
/// Returns pointer to float array (caller must free with embeddings_free_result)
#[no_mangle]
pub extern "C" fn embeddings_encode(
    handle: *mut EmbeddingsHandle,
    text: *const c_char,
    embeddings_out: *mut *mut f32,
    len_out: *mut usize,
) -> bool {
    if handle.is_null() || text.is_null() || embeddings_out.is_null() || len_out.is_null() {
        return false;
    }

    let handle = unsafe { &mut *handle };
    let c_str = unsafe { CStr::from_ptr(text) };
    let text = match c_str.to_str() {
        Ok(s) => s,
        Err(_) => return false,
    };

    // Tokenize with truncation (BGE models use 512 max sequence length)
    let encoding = match handle.tokenizer.encode(text, false) {
        Ok(e) => e,
        Err(e) => {
            eprintln!("[RAYON] Tokenization failed: {}", e);
            return false;
        }
    };

    const MAX_SEQ_LENGTH: usize = 512;
    let mut input_ids = encoding.get_ids();
    let mut attention_mask = encoding.get_attention_mask();
    let mut token_type_ids = encoding.get_type_ids();

    // Truncate if needed
    if input_ids.len() > MAX_SEQ_LENGTH {
        eprintln!("[RAYON] Truncating sequence from {} to {} tokens", input_ids.len(), MAX_SEQ_LENGTH);
        input_ids = &input_ids[..MAX_SEQ_LENGTH];
        attention_mask = &attention_mask[..MAX_SEQ_LENGTH];
        token_type_ids = &token_type_ids[..MAX_SEQ_LENGTH];
    }

    // Prepare tract inputs (i64 tensors)
    let input_ids_array = tract_ndarray::Array2::from_shape_vec(
        (1, input_ids.len()),
        input_ids.iter().map(|&x| x as i64).collect(),
    ).unwrap();

    let attention_mask_array = tract_ndarray::Array2::from_shape_vec(
        (1, attention_mask.len()),
        attention_mask.iter().map(|&x| x as i64).collect(),
    ).unwrap();

    let token_type_ids_array = tract_ndarray::Array2::from_shape_vec(
        (1, token_type_ids.len()),
        token_type_ids.iter().map(|&x| x as i64).collect(),
    ).unwrap();

    // Run inference (tract sessions are thread-safe)
    let outputs = match handle.model.run(tvec!(
        Tensor::from(input_ids_array).into(),
        Tensor::from(attention_mask_array).into(),
        Tensor::from(token_type_ids_array).into(),
    )) {
        Ok(o) => o,
        Err(e) => {
            eprintln!("[RAYON] ONNX inference failed: {}", e);
            return false;
        }
    };

    // Extract embeddings
    let mut embeddings = match outputs[0].to_array_view::<f32>() {
        Ok(tensor) => {
            let shape = tensor.shape();
            let seq_len = shape[1];
            let embedding_dim = shape[2];

            // Mean pooling over sequence dimension
            let mut pooled = vec![0.0f32; embedding_dim];
            for i in 0..seq_len {
                for j in 0..embedding_dim {
                    pooled[j] += tensor[[0, i, j]];
                }
            }
            for val in pooled.iter_mut() {
                *val /= seq_len as f32;
            }
            pooled
        }
        Err(e) => {
            eprintln!("[RAYON] Failed to extract embeddings: {}", e);
            return false;
        }
    };

    // Always normalize for BGE models
    normalize_vector(&mut embeddings);

    // Allocate output
    let len = embeddings.len();
    let mut boxed = embeddings.into_boxed_slice();
    let ptr = boxed.as_mut_ptr();
    std::mem::forget(boxed);

    unsafe {
        *embeddings_out = ptr;
        *len_out = len;
    }

    true
}

/// Encode batch of texts to normalized embeddings (parallel with rayon)
#[no_mangle]
pub extern "C" fn embeddings_encode_batch(
    handle: *mut EmbeddingsHandle,
    texts: *const *const c_char,
    num_texts: usize,
    embeddings_out: *mut *mut f32,
    len_out: *mut usize,
) -> bool {
    if handle.is_null() || texts.is_null() || embeddings_out.is_null() || len_out.is_null() {
        return false;
    }

    let handle = unsafe { &mut *handle };
    let texts_slice = unsafe { std::slice::from_raw_parts(texts, num_texts) };

    // Convert C strings to Rust strings first (so we can safely parallelize)
    let mut text_strings = Vec::with_capacity(num_texts);
    for (idx, &text_ptr) in texts_slice.iter().enumerate() {
        if text_ptr.is_null() {
            eprintln!("[RAYON] Text {} is null", idx);
            return false;
        }
        let c_str = unsafe { CStr::from_ptr(text_ptr) };
        match c_str.to_str() {
            Ok(s) => text_strings.push(s.to_string()),
            Err(e) => {
                eprintln!("[RAYON] Text {} UTF-8 conversion failed: {}", idx, e);
                return false;
            }
        }
    }

    let start = std::time::Instant::now();

    // Process texts in parallel using rayon
    let results: Vec<Result<Vec<f32>, String>> = handle.pool.install(|| {
        text_strings.par_iter().enumerate().map(|(idx, text)| {
            let iter_start = std::time::Instant::now();

            let tok_start = std::time::Instant::now();
            let encoding = handle.tokenizer.encode(text.as_str(), false)
                .map_err(|e| format!("Text {} tokenization failed: {}", idx, e))?;
            let tok_ms = tok_start.elapsed().as_millis();

            let tensor_start = std::time::Instant::now();

            const MAX_SEQ_LENGTH: usize = 512;
            let input_ids = encoding.get_ids();
            let attention_mask = encoding.get_attention_mask();
            let token_type_ids = encoding.get_type_ids();

            let seq_len = input_ids.len().min(MAX_SEQ_LENGTH);

            let input_ids_array = tract_ndarray::Array2::from_shape_vec(
                (1, seq_len),
                input_ids[..seq_len].iter().map(|&x| x as i64).collect(),
            ).unwrap();

            let attention_mask_array = tract_ndarray::Array2::from_shape_vec(
                (1, seq_len),
                attention_mask[..seq_len].iter().map(|&x| x as i64).collect(),
            ).unwrap();

            let token_type_ids_array = tract_ndarray::Array2::from_shape_vec(
                (1, seq_len),
                token_type_ids[..seq_len].iter().map(|&x| x as i64).collect(),
            ).unwrap();

            let tensor_ms = tensor_start.elapsed().as_millis();

            // Run inference (tract sessions are thread-safe)
            let infer_start = std::time::Instant::now();
            let outputs = handle.model.run(tvec!(
                Tensor::from(input_ids_array).into(),
                Tensor::from(attention_mask_array).into(),
                Tensor::from(token_type_ids_array).into(),
            )).map_err(|e| format!("Text {} inference failed: {}", idx, e))?;
            let infer_ms = infer_start.elapsed().as_millis();

            // Extract and pool embeddings
            let tensor = outputs[0].to_array_view::<f32>()
                .map_err(|e| format!("Text {} failed to extract embeddings: {}", idx, e))?;

            let shape = tensor.shape();
            let embedding_dim = shape[2];

            let mut pooled = vec![0.0f32; embedding_dim];
            for i in 0..seq_len {
                for j in 0..embedding_dim {
                    pooled[j] += tensor[[0, i, j]];
                }
            }
            for val in pooled.iter_mut() {
                *val /= seq_len as f32;
            }

            // Normalize
            normalize_vector(&mut pooled);

            let total_ms = iter_start.elapsed().as_millis();
            eprintln!("[RAYON] Text {}: tok={}ms tensor={}ms infer={}ms total={}ms",
                idx, tok_ms, tensor_ms, infer_ms, total_ms);

            Ok(pooled)
        }).collect()
    });

    let elapsed_ms = start.elapsed().as_millis();
    eprintln!("[RAYON] Processed {} texts in {}ms ({:.1} texts/sec)",
        num_texts, elapsed_ms, num_texts as f64 / (elapsed_ms as f64 / 1000.0));

    // Check for errors
    let mut all_embeddings = Vec::with_capacity(num_texts * handle.embedding_dim);
    for (_, result) in results.into_iter().enumerate() {
        match result {
            Ok(embedding) => all_embeddings.extend(embedding),
            Err(e) => {
                eprintln!("[RAYON] {}", e);
                return false;
            }
        }
    }

    if all_embeddings.is_empty() {
        eprintln!("[RAYON] Batch encoding failed: no embeddings generated");
        return false;
    }

    let len = all_embeddings.len();
    let mut boxed = all_embeddings.into_boxed_slice();
    let ptr = boxed.as_mut_ptr();
    std::mem::forget(boxed);

    unsafe {
        *embeddings_out = ptr;
        *len_out = len;
    }

    true
}

/// Free embeddings result
#[no_mangle]
pub extern "C" fn embeddings_free_result(embeddings: *mut f32, len: usize) {
    if !embeddings.is_null() && len > 0 {
        unsafe {
            let _ = Vec::from_raw_parts(embeddings, len, len);
        }
    }
}

/// Free embeddings handle
#[no_mangle]
pub extern "C" fn embeddings_free(handle: *mut EmbeddingsHandle) {
    if !handle.is_null() {
        unsafe {
            let _ = Box::from_raw(handle);
        }
    }
}

/// Get embedding dimension
#[no_mangle]
pub extern "C" fn embeddings_get_dimension(handle: *const EmbeddingsHandle) -> usize {
    if handle.is_null() {
        return 0;
    }
    let handle = unsafe { &*handle };
    handle.embedding_dim
}
