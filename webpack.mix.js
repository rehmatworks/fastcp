let mix = require('laravel-mix');

mix.override((config) => {
    delete config.watchOptions;
});

mix.js('resources/js/fastcp.js', 'core/static/core/assets/js').vue({ version: 3 });