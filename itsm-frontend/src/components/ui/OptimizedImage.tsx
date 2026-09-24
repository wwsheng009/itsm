/**
 * 优化的图片组件
 * 基于原生 img 封装，提供懒加载、占位与错误回退体验
 */

import React from 'react';
import { Skeleton } from 'antd';

export interface OptimizedImageProps {
  src: string;
  alt: string;
  width?: number;
  height?: number;
  fill?: boolean;
  /** 兼容保留：原生 img 不再区分优先级，仅影响 loading 取值 */
  priority?: boolean;
  /** 兼容保留：原生 img 不做质量压缩 */
  quality?: number;
  /** 兼容保留：原生 img 不支持模糊占位 */
  placeholder?: 'blur' | 'empty';
  /** 兼容保留：原生 img 不支持 blurDataURL */
  blurDataURL?: string;
  className?: string;
  objectFit?: 'contain' | 'cover' | 'fill' | 'none' | 'scale-down';
  objectPosition?: string;
  sizes?: string;
  loading?: 'eager' | 'lazy';
  onLoad?: () => void;
  onError?: () => void;
  fallbackSrc?: string;
  showSkeleton?: boolean;
}

/**
 * 优化的图片组件
 *
 * 特性：
 * - 自动图片优化
 * - 懒加载
 * - 占位符
 * - 错误处理
 * - 加载状态
 *
 * @example
 * ```tsx
 * <OptimizedImage
 *   src="/avatar.jpg"
 *   alt="User Avatar"
 *   width={100}
 *   height={100}
 *   placeholder="blur"
 * />
 * ```
 */
export const OptimizedImage: React.FC<OptimizedImageProps> = ({
  src,
  alt,
  width,
  height,
  fill = false,
  className = '',
  objectFit = 'cover',
  objectPosition = 'center',
  sizes,
  loading = 'lazy',
  onLoad,
  onError,
  fallbackSrc = '/images/placeholder.png',
  showSkeleton = true,
}) => {
  const [isLoading, setIsLoading] = React.useState(true);
  const [hasError, setHasError] = React.useState(false);
  const [imageSrc, setImageSrc] = React.useState(src);

  React.useEffect(() => {
    setImageSrc(src);
    setHasError(false);
    setIsLoading(true);
  }, [src]);

  const handleLoad = () => {
    setIsLoading(false);
    onLoad?.();
  };

  const handleError = () => {
    setHasError(true);
    setIsLoading(false);
    setImageSrc(fallbackSrc);
    onError?.();
  };

  return (
    <div className={`relative ${className}`} style={{ width, height }}>
      {showSkeleton && isLoading && (
        <div className="absolute inset-0 z-10">
          <Skeleton.Image
            active
            style={{
              width: '100%',
              height: '100%',
            }}
          />
        </div>
      )}

      <img
        src={imageSrc}
        alt={alt}
        width={fill ? undefined : width || 100}
        height={fill ? undefined : height || 100}
        sizes={sizes || (fill ? '100vw' : undefined)}
        loading={loading}
        style={{
          objectFit,
          objectPosition,
          ...(fill
            ? { position: 'absolute', inset: 0, width: '100%', height: '100%' }
            : undefined),
        }}
        onLoad={handleLoad}
        onError={handleError}
        className={`
          transition-opacity duration-300
          ${isLoading ? 'opacity-0' : 'opacity-100'}
          ${hasError ? 'filter grayscale' : ''}
        `}
      />
    </div>
  );
};

/**
 * 头像组件
 * 专门用于显示用户头像
 */
export interface AvatarImageProps {
  src?: string;
  name: string;
  size?: number;
  shape?: 'circle' | 'square';
  className?: string;
}

export const AvatarImage: React.FC<AvatarImageProps> = ({
  src,
  name,
  size = 40,
  shape = 'circle',
  className = '',
}) => {
  const [hasError, setHasError] = React.useState(false);

  // 生成默认头像（使用用户名首字母）
  const generateDefaultAvatar = () => {
    const initial = name.charAt(0).toUpperCase();
    const colors = [
      'bg-blue-500',
      'bg-green-500',
      'bg-yellow-500',
      'bg-red-500',
      'bg-purple-500',
      'bg-pink-500',
      'bg-indigo-500',
    ];
    const colorIndex = name.charCodeAt(0) % colors.length;

    return (
      <div
        className={`
          flex items-center justify-center
          ${colors[colorIndex]} text-white font-semibold
          ${shape === 'circle' ? 'rounded-full' : 'rounded-md'}
          ${className}
        `}
        style={{ width: size, height: size, fontSize: size * 0.4 }}
      >
        {initial}
      </div>
    );
  };

  if (!src || hasError) {
    return generateDefaultAvatar();
  }

  return (
    <div
      className={`
        relative overflow-hidden
        ${shape === 'circle' ? 'rounded-full' : 'rounded-md'}
        ${className}
      `}
      style={{ width: size, height: size }}
    >
      <img
        src={src}
        alt={name}
        width={size}
        height={size}
        loading="lazy"
        style={{
          objectFit: 'cover',
        }}
        onError={() => setHasError(true)}
      />
    </div>
  );
};

/**
 * 缩略图组件
 * 用于显示文件或图片缩略图
 */
export interface ThumbnailImageProps {
  src: string;
  alt: string;
  width?: number;
  height?: number;
  className?: string;
  onClick?: () => void;
}

export const ThumbnailImage: React.FC<ThumbnailImageProps> = ({
  src,
  alt,
  width = 80,
  height = 80,
  className = '',
  onClick,
}) => {
  return (
    <div
      className={`
        relative overflow-hidden rounded-md
        border border-gray-200 hover:border-blue-400
        transition-all cursor-pointer
        ${className}
      `}
      style={{ width, height }}
      onClick={onClick}
    >
      <OptimizedImage
        src={src}
        alt={alt}
        width={width}
        height={height}
        objectFit="cover"
        quality={60}
        loading="lazy"
      />
    </div>
  );
};

/**
 * 背景图片组件
 * 用于大背景图片
 */
export interface BackgroundImageProps {
  src: string;
  alt: string;
  priority?: boolean;
  overlay?: boolean;
  overlayOpacity?: number;
  children?: React.ReactNode;
  className?: string;
}

export const BackgroundImage: React.FC<BackgroundImageProps> = ({
  src,
  alt,
  priority = false,
  overlay = false,
  overlayOpacity = 0.5,
  children,
  className = '',
}) => {
  return (
    <div className={`relative w-full h-full ${className}`}>
      <img
        src={src}
        alt={alt}
        loading={priority ? 'eager' : 'lazy'}
        sizes="100vw"
        style={{
          position: 'absolute',
          inset: 0,
          width: '100%',
          height: '100%',
          objectFit: 'cover',
        }}
      />

      {overlay && <div className="absolute inset-0 bg-black" style={{ opacity: overlayOpacity }} />}

      {children && <div className="relative z-10">{children}</div>}
    </div>
  );
};

/**
 * Logo 组件
 * 用于显示公司/品牌 Logo
 */
export interface LogoImageProps {
  src: string;
  alt: string;
  width?: number;
  height?: number;
  priority?: boolean;
  className?: string;
}

export const LogoImage: React.FC<LogoImageProps> = ({
  src,
  alt,
  width = 120,
  height = 40,
  priority = true,
  className = '',
}) => {
  return (
    <OptimizedImage
      src={src}
      alt={alt}
      width={width}
      height={height}
      priority={priority}
      quality={90}
      objectFit="contain"
      className={className}
    />
  );
};

// 导出所有组件
export default OptimizedImage;
