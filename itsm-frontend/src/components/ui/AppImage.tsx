import React from 'react';

interface AppImageProps
  extends Omit<React.ImgHTMLAttributes<HTMLImageElement>, 'src' | 'alt' | 'loading'> {
  src: string;
  alt: string;
  /** 是否启用懒加载，默认 true */
  lazy?: boolean;
  /** 加载失败时的占位图 */
  fallbackSrc?: string;
}

export default function AppImage({
  alt,
  lazy = true,
  fallbackSrc = '/images/placeholder.png',
  ...props
}: AppImageProps) {
  const [imgSrc, setImgSrc] = React.useState<string>(props.src);

  const handleError = (event: React.SyntheticEvent<HTMLImageElement, Event>) => {
    setImgSrc(fallbackSrc);
    props.onError?.(event);
  };

  return (
    <img
      {...props}
      alt={alt}
      loading={lazy ? 'lazy' : 'eager'}
      src={imgSrc}
      onError={handleError}
    />
  );
}
